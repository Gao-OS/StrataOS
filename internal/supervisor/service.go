package supervisor

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// ServiceConfig describes how to launch and manage a service.
type ServiceConfig struct {
	Name         string
	BinaryPath   string
	SocketName   string        // e.g. "identity.sock"
	DependsOn    []string      // names of services that must be Healthy first
	ReadyTimeout time.Duration // how long to wait for socket readiness
}

// ServiceEntry tracks a running service's state and process.
type ServiceEntry struct {
	Config      ServiceConfig
	State       ServiceState
	Cmd         *exec.Cmd
	PID         int
	CrashCount  int
	CrashWindow []time.Time
	runtimeDir  string
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
func (se *ServiceEntry) transition(to ServiceState) error {
	if !CanTransition(se.State, to) {
		return fmt.Errorf("illegal transition for %s: %s -> %s", se.Config.Name, se.State, to)
	}
	se.State = to
	return nil
}

// start spawns the service process and launches a monitor goroutine.
// crashCh receives the service name when the process exits unexpectedly.
func (se *ServiceEntry) start(env []string, crashCh chan<- string) error {
	if err := se.transition(Starting); err != nil {
		return err
	}

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
	log.Printf("[supervisor] started %s (pid=%d)", se.Config.Name, se.PID)

	// Monitor goroutine: waits for process exit.
	go se.monitor(crashCh)

	// Wait for socket readiness.
	sockPath := filepath.Join(se.runtimeDir, se.Config.SocketName)
	if !waitForFile(sockPath, se.Config.ReadyTimeout) {
		log.Printf("[supervisor] %s did not become ready (timeout waiting for %s)", se.Config.Name, sockPath)
		se.stop(0)
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
func (se *ServiceEntry) monitor(crashCh chan<- string) {
	if se.Cmd == nil || se.Cmd.Process == nil {
		return
	}
	err := se.Cmd.Wait()
	if se.State == Stopped {
		return // intentional shutdown, not a crash
	}
	if err != nil {
		log.Printf("[supervisor] %s exited: %v", se.Config.Name, err)
	} else {
		log.Printf("[supervisor] %s exited unexpectedly (exit 0)", se.Config.Name)
	}
	crashCh <- se.Config.Name
}

// stop sends SIGTERM, waits for drainMs, then SIGKILL if still alive.
func (se *ServiceEntry) stop(drainMs int) {
	if se.Cmd == nil || se.Cmd.Process == nil {
		return
	}
	log.Printf("[supervisor] stopping %s (pid=%d)", se.Config.Name, se.PID)
	se.Cmd.Process.Signal(syscall.SIGTERM)

	if drainMs <= 0 {
		drainMs = 2000
	}
	done := make(chan struct{})
	go func() {
		se.Cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Process exited cleanly.
	case <-time.After(time.Duration(drainMs) * time.Millisecond):
		log.Printf("[supervisor] %s did not exit after %dms, sending SIGKILL", se.Config.Name, drainMs)
		se.Cmd.Process.Kill()
		<-done
	}
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
