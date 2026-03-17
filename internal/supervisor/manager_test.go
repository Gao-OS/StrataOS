package supervisor

import (
	"testing"
	"time"
)

func TestDeclare(t *testing.T) {
	m := NewManager(ManagerConfig{RuntimeDir: "/tmp/test-strata"})
	err := m.Declare(ServiceConfig{Name: "svc1", BinaryPath: "/bin/true", SocketName: "svc1.sock"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Duplicate declaration should fail.
	err = m.Declare(ServiceConfig{Name: "svc1", BinaryPath: "/bin/true", SocketName: "svc1.sock"})
	if err == nil {
		t.Error("expected error for duplicate declaration")
	}
}

func TestListServices(t *testing.T) {
	m := NewManager(ManagerConfig{RuntimeDir: "/tmp/test-strata"})
	m.Declare(ServiceConfig{Name: "a", BinaryPath: "/bin/true", SocketName: "a.sock"})
	m.Declare(ServiceConfig{Name: "b", BinaryPath: "/bin/true", SocketName: "b.sock"})

	list := m.ListServices()
	if len(list) != 2 {
		t.Fatalf("expected 2 services, got %d", len(list))
	}

	found := map[string]bool{}
	for _, s := range list {
		found[s.Name] = true
		if s.State != "Declared" {
			t.Errorf("service %s should be Declared, got %s", s.Name, s.State)
		}
	}
	if !found["a"] || !found["b"] {
		t.Errorf("missing expected services: %v", found)
	}
}

func TestStatus(t *testing.T) {
	m := NewManager(ManagerConfig{RuntimeDir: "/tmp/test-strata", NodeID: "node-1"})
	status := m.Status()
	if status["node_id"] != "node-1" {
		t.Errorf("expected node_id=node-1, got %v", status["node_id"])
	}
	uptime, ok := status["uptime_sec"].(int)
	if !ok || uptime < 0 {
		t.Errorf("unexpected uptime: %v", status["uptime_sec"])
	}
}

func TestTopoSort_NoDeps(t *testing.T) {
	m := NewManager(ManagerConfig{RuntimeDir: "/tmp/test-strata"})
	m.Declare(ServiceConfig{Name: "a", BinaryPath: "/bin/true", SocketName: "a.sock"})
	m.Declare(ServiceConfig{Name: "b", BinaryPath: "/bin/true", SocketName: "b.sock"})

	order, err := m.topoSort()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(order))
	}
}

func TestTopoSort_WithDeps(t *testing.T) {
	m := NewManager(ManagerConfig{RuntimeDir: "/tmp/test-strata"})
	m.Declare(ServiceConfig{Name: "fs", BinaryPath: "/bin/true", SocketName: "fs.sock", DependsOn: []string{"identity"}})
	m.Declare(ServiceConfig{Name: "identity", BinaryPath: "/bin/true", SocketName: "identity.sock"})
	m.Declare(ServiceConfig{Name: "registry", BinaryPath: "/bin/true", SocketName: "registry.sock"})

	order, err := m.topoSort()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(order))
	}

	// identity must come before fs.
	idIdx, fsIdx := -1, -1
	for i, name := range order {
		if name == "identity" {
			idIdx = i
		}
		if name == "fs" {
			fsIdx = i
		}
	}
	if idIdx >= fsIdx {
		t.Errorf("identity (idx=%d) should come before fs (idx=%d)", idIdx, fsIdx)
	}
}

func TestTopoSort_CycleDetection(t *testing.T) {
	m := NewManager(ManagerConfig{RuntimeDir: "/tmp/test-strata"})
	m.Declare(ServiceConfig{Name: "a", BinaryPath: "/bin/true", SocketName: "a.sock", DependsOn: []string{"b"}})
	m.Declare(ServiceConfig{Name: "b", BinaryPath: "/bin/true", SocketName: "b.sock", DependsOn: []string{"a"}})

	_, err := m.topoSort()
	if err == nil {
		t.Error("expected cycle detection error")
	}
}

func TestTopoSort_UnknownDep(t *testing.T) {
	m := NewManager(ManagerConfig{RuntimeDir: "/tmp/test-strata"})
	m.Declare(ServiceConfig{Name: "a", BinaryPath: "/bin/true", SocketName: "a.sock", DependsOn: []string{"nonexistent"}})

	_, err := m.topoSort()
	if err == nil {
		t.Error("expected error for unknown dependency")
	}
}

func TestHandleCrash_Quarantine(t *testing.T) {
	m := NewManager(ManagerConfig{
		RuntimeDir: "/tmp/test-strata",
		Quarantine: QuarantineConfig{MaxCrashes: 3, Window: time.Minute},
	})
	m.Declare(ServiceConfig{Name: "svc", BinaryPath: "/bin/true", SocketName: "svc.sock"})
	se := m.services["svc"]

	se.mu.Lock()
	se.State = Healthy
	// Simulate rapid crashes up to quarantine threshold.
	now := time.Now()
	se.CrashWindow = []time.Time{
		now.Add(-30 * time.Second),
		now.Add(-15 * time.Second),
	}
	se.CrashCount = 2
	se.mu.Unlock()

	// This crash should trigger quarantine (3rd crash within window).
	m.handleCrash("svc")

	// Give time.AfterFunc a moment (it shouldn't fire for quarantine).
	time.Sleep(50 * time.Millisecond)

	se.mu.Lock()
	state := se.State
	se.mu.Unlock()
	if state != Quarantined {
		t.Errorf("expected Quarantined, got %s", state)
	}
}

func TestHandleCrash_Restarting(t *testing.T) {
	m := NewManager(ManagerConfig{
		RuntimeDir: "/tmp/test-strata",
		Quarantine: QuarantineConfig{MaxCrashes: 10, Window: time.Minute},
	})
	m.Declare(ServiceConfig{Name: "svc", BinaryPath: "/bin/true", SocketName: "svc.sock"})
	se := m.services["svc"]

	se.mu.Lock()
	se.State = Healthy
	se.mu.Unlock()

	m.handleCrash("svc")

	se.mu.Lock()
	state := se.State
	crashCount := se.CrashCount
	se.mu.Unlock()

	// Should transition to Restarting (not Quarantined, since under threshold).
	if state != Restarting {
		t.Errorf("expected Restarting, got %s", state)
	}
	if crashCount != 1 {
		t.Errorf("expected CrashCount=1, got %d", crashCount)
	}
}

func TestHandleCrash_Stopped(t *testing.T) {
	m := NewManager(ManagerConfig{RuntimeDir: "/tmp/test-strata"})
	m.Declare(ServiceConfig{Name: "svc", BinaryPath: "/bin/true", SocketName: "svc.sock"})
	se := m.services["svc"]

	se.mu.Lock()
	se.State = Stopped
	se.mu.Unlock()

	// Should not change state when already stopped.
	m.handleCrash("svc")

	se.mu.Lock()
	state := se.State
	se.mu.Unlock()
	if state != Stopped {
		t.Errorf("expected Stopped, got %s", state)
	}
}

func TestHandleCrash_PrunesCrashWindow(t *testing.T) {
	m := NewManager(ManagerConfig{
		RuntimeDir: "/tmp/test-strata",
		Quarantine: QuarantineConfig{MaxCrashes: 10, Window: time.Minute},
	})
	m.Declare(ServiceConfig{Name: "svc", BinaryPath: "/bin/true", SocketName: "svc.sock"})
	se := m.services["svc"]

	se.mu.Lock()
	se.State = Healthy
	// Add old crash entries that should be pruned.
	se.CrashWindow = []time.Time{
		time.Now().Add(-10 * time.Minute),
		time.Now().Add(-5 * time.Minute),
	}
	se.mu.Unlock()

	m.handleCrash("svc")

	se.mu.Lock()
	windowLen := len(se.CrashWindow)
	se.mu.Unlock()

	// Old entries pruned, only the new crash should remain.
	if windowLen != 1 {
		t.Errorf("expected CrashWindow length 1 after pruning, got %d", windowLen)
	}
}

func TestStopService_NotRunning(t *testing.T) {
	m := NewManager(ManagerConfig{RuntimeDir: "/tmp/test-strata"})
	m.Declare(ServiceConfig{Name: "svc", BinaryPath: "/bin/true", SocketName: "svc.sock"})

	err := m.StopService("svc", 1000)
	if err == nil {
		t.Error("expected error stopping non-running service")
	}
}

func TestStopService_Unknown(t *testing.T) {
	m := NewManager(ManagerConfig{RuntimeDir: "/tmp/test-strata"})
	err := m.StopService("nonexistent", 1000)
	if err == nil {
		t.Error("expected error for unknown service")
	}
}

func TestStartService_UnknownService(t *testing.T) {
	m := NewManager(ManagerConfig{RuntimeDir: "/tmp/test-strata"})
	err := m.StartService("nonexistent")
	if err == nil {
		t.Error("expected error for unknown service")
	}
}

func TestStartService_DepNotHealthy(t *testing.T) {
	m := NewManager(ManagerConfig{RuntimeDir: "/tmp/test-strata"})
	m.Declare(ServiceConfig{Name: "identity", BinaryPath: "/bin/true", SocketName: "identity.sock"})
	m.Declare(ServiceConfig{Name: "fs", BinaryPath: "/bin/true", SocketName: "fs.sock", DependsOn: []string{"identity"}})

	err := m.StartService("fs")
	if err == nil {
		t.Error("expected error when dependency is not healthy")
	}
}
