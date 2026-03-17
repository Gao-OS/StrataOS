package supervisor

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// defaultDrainMs is the default time to wait for a service to exit after SIGTERM.
const defaultDrainMs = 2000

// ServiceConfig describes how to launch and manage a service.
type ServiceConfig struct {
	Name         string
	BinaryPath   string
	SocketName   string        // e.g. "identity.sock"
	DependsOn    []string      // names of services that must be Healthy first
	ReadyTimeout time.Duration // how long to wait for socket readiness
}

// ServiceEntry tracks a running service's state and process.
// All mutable fields are protected by mu.
type ServiceEntry struct {
	mu          sync.Mutex
	Config      ServiceConfig
	State       ServiceState
	Cmd         *exec.Cmd
	PID         int
	CrashCount  int
	CrashWindow []time.Time
	runtimeDir  string
	done        chan struct{} // closed when the process exits (by monitor)
}

// newServiceEntry creates a ServiceEntry in Declared state.
func newServiceEntry(cfg ServiceConfig, runtimeDir string) *ServiceEntry {
	if cfg.ReadyTimeout == 0 {
		cfg.ReadyTimeout = 5 * time.Second
	}
	return &ServiceEntry{
		Config:     cfg,
		State:      Declared,
		runtimeDir: runtimeDir,
	}
}

// transition attempts a state change, returning an error for illegal transitions.
// Caller must hold se.mu.
func (se *ServiceEntry) transition(to ServiceState) error {
	if !CanTransition(se.State, to) {
		return fmt.Errorf("illegal transition for %s: %s -> %s", se.Config.Name, se.State, to)
	}
	se.State = to
	return nil
}

// start spawns the service process and launches a monitor goroutine.
// crashCh receives the service name when the process exits unexpectedly.
// Caller must hold se.mu.
func (se *ServiceEntry) start(env []string, crashCh chan<- string) error {
	if err := se.transition(Starting); err != nil {
		return err
	}

	// Remove stale socket before starting, in case a previous crash left it behind.
	sockPath := filepath.Join(se.runtimeDir, se.Config.SocketName)
	os.Remove(sockPath)

	cmd := exec.Command(se.Config.BinaryPath)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		se.State = Crashed
		return fmt.Errorf("start %s: %w", se.Config.Name, err)
	}

	se.Cmd = cmd
	se.PID = cmd.Process.Pid
	se.done = make(chan struct{})
	log.Printf("[supervisor] started %s (pid=%d)", se.Config.Name, se.PID)

	// Monitor goroutine: the sole goroutine that calls cmd.Wait().
	go se.monitor(crashCh)

	// Wait for socket readiness (don't hold the lock during polling).
	se.mu.Unlock()
	ready := waitForFile(sockPath, se.Config.ReadyTimeout)
	se.mu.Lock()

	if !ready {
		log.Printf("[supervisor] %s did not become ready (timeout waiting for %s)", se.Config.Name, sockPath)
		se.stopLocked(0)
		se.State = Crashed
		return fmt.Errorf("%s did not become ready", se.Config.Name)
	}

	if err := se.transition(Healthy); err != nil {
		return err
	}
	log.Printf("[supervisor] %s healthy", se.Config.Name)
	return nil
}

// monitor waits for the process to exit and reports crashes.
// This is the ONLY goroutine that calls cmd.Wait().
func (se *ServiceEntry) monitor(crashCh chan<- string) {
	if se.Cmd == nil || se.Cmd.Process == nil {
		return
	}
	err := se.Cmd.Wait()

	// Signal that the process has exited (used by stopLocked to wait).
	close(se.done)

	se.mu.Lock()
	state := se.State
	se.mu.Unlock()

	if state == Stopped {
		return // intentional shutdown, not a crash
	}
	if err != nil {
		log.Printf("[supervisor] %s exited: %v", se.Config.Name, err)
	} else {
		log.Printf("[supervisor] %s exited unexpectedly (exit 0)", se.Config.Name)
	}
	crashCh <- se.Config.Name
}

// stopLocked sends SIGTERM, waits for drainMs, then SIGKILL if still alive.
// Caller must hold se.mu; lock is released during the wait.
func (se *ServiceEntry) stopLocked(drainMs int) {
	if se.Cmd == nil || se.Cmd.Process == nil {
		return
	}
	log.Printf("[supervisor] stopping %s (pid=%d)", se.Config.Name, se.PID)
	se.Cmd.Process.Signal(syscall.SIGTERM)

	if drainMs <= 0 {
		drainMs = defaultDrainMs
	}

	done := se.done
	// Release the lock while waiting for the process to exit.
	se.mu.Unlock()

	select {
	case <-done:
		// Process exited cleanly (monitor closed the channel).
	case <-time.After(time.Duration(drainMs) * time.Millisecond):
		log.Printf("[supervisor] %s did not exit after %dms, sending SIGKILL", se.Config.Name, drainMs)
		se.Cmd.Process.Kill()
		<-done
	}

	se.mu.Lock()
}

// waitForFile polls for a file's existence until timeout.
func waitForFile(path string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}
