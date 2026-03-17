package supervisor

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// Manager orchestrates service lifecycle with dependency ordering,
// crash recovery, and quarantine.
type Manager struct {
	mu         sync.RWMutex
	services   map[string]*ServiceEntry
	order      []string // topological start order
	runtimeDir string
	nodeID     string
	startTime  time.Time
	env        []string // captured once at construction
	crashCh    chan string
	backoff    BackoffConfig
	quarantine QuarantineConfig
	onHealthy  func(name string) // callback when a service becomes healthy
}

// ManagerConfig configures the Manager.
type ManagerConfig struct {
	RuntimeDir string
	NodeID     string
	Backoff    BackoffConfig
	Quarantine QuarantineConfig
	OnHealthy  func(name string)
}

// NewManager creates a Manager with the given configuration.
func NewManager(cfg ManagerConfig) *Manager {
	if cfg.Backoff == (BackoffConfig{}) {
		cfg.Backoff = DefaultBackoff()
	}
	if cfg.Quarantine == (QuarantineConfig{}) {
		cfg.Quarantine = DefaultQuarantine()
	}

	// Capture environment once to avoid divergence on restarts.
	env := append(os.Environ(), fmt.Sprintf("STRATA_RUNTIME_DIR=%s", cfg.RuntimeDir))
	if cfg.NodeID != "" {
		env = append(env, fmt.Sprintf("STRATA_NODE_ID=%s", cfg.NodeID))
	}

	return &Manager{
		services:   make(map[string]*ServiceEntry),
		runtimeDir: cfg.RuntimeDir,
		nodeID:     cfg.NodeID,
		startTime:  time.Now(),
		env:        env,
		crashCh:    make(chan string, 16),
		backoff:    cfg.Backoff,
		quarantine: cfg.Quarantine,
		onHealthy:  cfg.OnHealthy,
	}
}

// Declare registers a service in Declared state. Must be called before StartAll.
func (m *Manager) Declare(cfg ServiceConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.services[cfg.Name]; exists {
		return fmt.Errorf("service %q already declared", cfg.Name)
	}
	m.services[cfg.Name] = newServiceEntry(cfg, m.runtimeDir)
	return nil
}

// StartAll starts services in dependency order. Services with no dependencies
// start first; services with DependsOn wait until their dependencies are Healthy.
func (m *Manager) StartAll() error {
	m.mu.Lock()
	order, err := m.topoSort()
	if err != nil {
		m.mu.Unlock()
		return err
	}
	m.order = order
	m.mu.Unlock()

	for _, name := range order {
		m.mu.RLock()
		se := m.services[name]
		m.mu.RUnlock()

		se.mu.Lock()
		err := se.start(m.env, m.crashCh)
		se.mu.Unlock()
		if err != nil {
			return fmt.Errorf("start %s: %w", name, err)
		}
		if m.onHealthy != nil {
			m.onHealthy(name)
		}
	}
	return nil
}

// StartService starts a single service by name.
func (m *Manager) StartService(name string) error {
	m.mu.RLock()
	se, ok := m.services[name]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown service %q", name)
	}

	// Verify dependencies are healthy.
	for _, dep := range se.Config.DependsOn {
		m.mu.RLock()
		depEntry, exists := m.services[dep]
		m.mu.RUnlock()
		if !exists {
			return fmt.Errorf("dependency %q is not healthy", dep)
		}
		depEntry.mu.Lock()
		depState := depEntry.State
		depEntry.mu.Unlock()
		if depState != Healthy {
			return fmt.Errorf("dependency %q is not healthy", dep)
		}
	}

	se.mu.Lock()
	err := se.start(m.env, m.crashCh)
	se.mu.Unlock()
	if err != nil {
		return err
	}
	if m.onHealthy != nil {
		m.onHealthy(name)
	}
	return nil
}

// StopService stops a single service.
func (m *Manager) StopService(name string, drainMs int) error {
	m.mu.RLock()
	se, ok := m.services[name]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown service %q", name)
	}

	se.mu.Lock()
	if se.State != Healthy && se.State != Starting {
		state := se.State
		se.mu.Unlock()
		return fmt.Errorf("service %q is not running (state=%s)", name, state)
	}
	se.State = Stopped
	se.stopLocked(drainMs)
	se.mu.Unlock()
	return nil
}

// StopAll stops all services in reverse dependency order.
func (m *Manager) StopAll() {
	m.mu.RLock()
	order := make([]string, len(m.order))
	copy(order, m.order)
	m.mu.RUnlock()

	// Reverse order.
	for i, j := 0, len(order)-1; i < j; i, j = i+1, j-1 {
		order[i], order[j] = order[j], order[i]
	}

	for _, name := range order {
		m.mu.RLock()
		se := m.services[name]
		m.mu.RUnlock()

		se.mu.Lock()
		if se.State == Healthy || se.State == Starting {
			se.State = Stopped
			se.stopLocked(defaultDrainMs)
		}
		se.mu.Unlock()
	}
}

// ServiceStatus describes a service for IPC responses.
type ServiceStatus struct {
	Name  string `json:"name"`
	State string `json:"state"`
	PID   int    `json:"pid"`
}

// ListServices returns the status of all declared services.
func (m *Manager) ListServices() []ServiceStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]ServiceStatus, 0, len(m.services))
	for _, se := range m.services {
		se.mu.Lock()
		result = append(result, ServiceStatus{
			Name:  se.Config.Name,
			State: se.State.String(),
			PID:   se.PID,
		})
		se.mu.Unlock()
	}
	return result
}

// Status returns overall supervisor status.
func (m *Manager) Status() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return map[string]any{
		"node_id":    m.nodeID,
		"uptime_sec": int(time.Since(m.startTime).Seconds()),
	}
}

// Run starts the crash recovery loop. Blocks until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case name := <-m.crashCh:
			m.handleCrash(name)
		}
	}
}

// handleCrash processes a service crash: checks quarantine, schedules restart.
func (m *Manager) handleCrash(name string) {
	m.mu.RLock()
	se, ok := m.services[name]
	m.mu.RUnlock()
	if !ok {
		return
	}

	se.mu.Lock()

	// Only transition if not already stopped.
	if se.State == Stopped {
		se.mu.Unlock()
		return
	}

	se.State = Crashed
	se.CrashCount++
	now := time.Now()
	se.CrashWindow = append(se.CrashWindow, now)

	// Prune crash window entries outside the quarantine window.
	cutoff := now.Add(-m.quarantine.Window)
	pruned := se.CrashWindow[:0]
	for _, t := range se.CrashWindow {
		if t.After(cutoff) {
			pruned = append(pruned, t)
		}
	}
	se.CrashWindow = pruned

	if ShouldQuarantine(se.CrashWindow, m.quarantine) {
		log.Printf("[supervisor] %s quarantined (%d crashes in window)", name, se.CrashCount)
		se.State = Quarantined
		se.mu.Unlock()
		return
	}

	delay := ComputeDelay(se.CrashCount, m.backoff)
	log.Printf("[supervisor] %s crashed (count=%d), restarting in %v", name, se.CrashCount, delay)
	se.State = Restarting
	se.mu.Unlock()

	time.AfterFunc(delay, func() {
		se.mu.Lock()
		if se.State != Restarting {
			se.mu.Unlock()
			return
		}
		// Transition Restarting → Starting happens inside start().
		se.State = Declared // reset so start() can transition Declared → Starting
		err := se.start(m.env, m.crashCh)
		se.mu.Unlock()

		if err != nil {
			log.Printf("[supervisor] restart %s failed: %v", name, err)
			return
		}
		if m.onHealthy != nil {
			m.onHealthy(name)
		}
	})
}

// topoSort returns service names in dependency order (Kahn's algorithm).
// Caller must hold m.mu.
func (m *Manager) topoSort() ([]string, error) {
	// Build in-degree map.
	inDegree := make(map[string]int)
	dependents := make(map[string][]string) // dep → services that depend on it

	for name, se := range m.services {
		if _, ok := inDegree[name]; !ok {
			inDegree[name] = 0
		}
		for _, dep := range se.Config.DependsOn {
			if _, exists := m.services[dep]; !exists {
				return nil, fmt.Errorf("service %q depends on unknown service %q", name, dep)
			}
			inDegree[name]++
			dependents[dep] = append(dependents[dep], name)
		}
	}

	// Start with nodes that have no dependencies.
	var queue []string
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}

	var order []string
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		order = append(order, name)

		for _, dependent := range dependents[name] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	if len(order) != len(m.services) {
		return nil, fmt.Errorf("dependency cycle detected")
	}
	return order, nil
}
