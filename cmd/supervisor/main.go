// Supervisor: top-level process that manages the Strata service lifecycle.
// Uses the Manager state machine for dependency-ordered startup, crash recovery,
// and quarantine. Exposes IPC control methods on supervisor.sock.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Gao-OS/StrataOS/internal/ipc"
	"github.com/Gao-OS/StrataOS/internal/supervisor"
)

func main() {
	runtimeDir := os.Getenv("STRATA_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = "/run/strata"
	}
	nodeID := os.Getenv("STRATA_NODE_ID")
	if nodeID == "" {
		nodeID = "local-0"
	}

	log.Printf("[supervisor] starting (runtime_dir=%s, node_id=%s)", runtimeDir, nodeID)

	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		log.Fatalf("[supervisor] create runtime dir: %v", err)
	}

	// Registry socket path for onHealthy registration.
	registrySock := filepath.Join(runtimeDir, "registry.sock")

	mgr := supervisor.NewManager(supervisor.ManagerConfig{
		RuntimeDir: runtimeDir,
		NodeID:     nodeID,
		Backoff:    supervisor.DefaultBackoff(),
		Quarantine: supervisor.DefaultQuarantine(),
		OnHealthy: func(name string) {
			// Auto-register healthy services in the registry (fire-and-forget).
			// Skip registry itself to avoid circular dependency.
			if name == "registry" {
				return
			}
			endpoint := fmt.Sprintf("unix://%s", filepath.Join(runtimeDir, name+".sock"))
			go registerInRegistry(registrySock, name, endpoint)
		},
	})

	// Locate and declare services.
	registryBin, err := findServiceBinary("registry")
	if err != nil {
		log.Fatalf("[supervisor] %v", err)
	}
	identityBin, err := findServiceBinary("identity")
	if err != nil {
		log.Fatalf("[supervisor] %v", err)
	}
	fsBin, err := findServiceBinary("fs")
	if err != nil {
		log.Fatalf("[supervisor] %v", err)
	}

	mgr.Declare(supervisor.ServiceConfig{
		Name:         "registry",
		BinaryPath:   registryBin,
		SocketName:   "registry.sock",
		ReadyTimeout: 5 * time.Second,
	})
	mgr.Declare(supervisor.ServiceConfig{
		Name:         "identity",
		BinaryPath:   identityBin,
		SocketName:   "identity.sock",
		ReadyTimeout: 5 * time.Second,
	})
	mgr.Declare(supervisor.ServiceConfig{
		Name:         "fs",
		BinaryPath:   fsBin,
		SocketName:   "fs.sock",
		DependsOn:    []string{"identity"},
		ReadyTimeout: 5 * time.Second,
	})

	if err := mgr.StartAll(); err != nil {
		log.Fatalf("[supervisor] %v", err)
	}

	// Control socket.
	ctlSrv := ipc.NewServer(filepath.Join(runtimeDir, "supervisor.sock"))

	ctlSrv.Handle("supervisor.status", func(req *ipc.Request) ipc.Response {
		return ipc.SuccessResponse(req.ReqID, mgr.Status())
	})

	ctlSrv.Handle("supervisor.svc.list", func(req *ipc.Request) ipc.Response {
		return ipc.SuccessResponse(req.ReqID, map[string]any{
			"services": mgr.ListServices(),
		})
	})

	ctlSrv.Handle("supervisor.svc.start", func(req *ipc.Request) ipc.Response {
		name, _ := req.Params["name"].(string)
		if name == "" {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrInvalidRequest, "missing name param")
		}
		if err := mgr.StartService(name); err != nil {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrInternal, err.Error())
		}
		return ipc.SuccessResponse(req.ReqID, map[string]string{"status": "started"})
	})

	ctlSrv.Handle("supervisor.svc.stop", func(req *ipc.Request) ipc.Response {
		name, _ := req.Params["name"].(string)
		if name == "" {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrInvalidRequest, "missing name param")
		}
		drainMs := 2000
		if d, ok := req.Params["drain_ms"].(float64); ok && d > 0 {
			drainMs = int(d)
		}
		if err := mgr.StopService(name, drainMs); err != nil {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrInternal, err.Error())
		}
		return ipc.SuccessResponse(req.ReqID, map[string]string{"status": "stopped"})
	})

	if err := ctlSrv.Start(); err != nil {
		log.Fatalf("[supervisor] control socket: %v", err)
	}

	log.Printf("[supervisor] all services running")

	// Crash recovery loop in background.
	ctx, cancel := context.WithCancel(context.Background())
	go mgr.Run(ctx)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Printf("[supervisor] shutting down")
	cancel()
	ctlSrv.Stop()
	mgr.StopAll()
}

// registerInRegistry sends a registry.register request (fire-and-forget).
func registerInRegistry(registrySock, service, endpoint string) {
	req := &ipc.Request{
		V:      1,
		ReqID:  fmt.Sprintf("register-%s", service),
		Method: "registry.register",
		Params: map[string]any{
			"service":  service,
			"endpoint": endpoint,
			"api_v":    1,
		},
	}
	resp, err := ipc.SendRequest(registrySock, req)
	if err != nil {
		log.Printf("[supervisor] failed to register %s in registry: %v", service, err)
		return
	}
	if !resp.OK {
		log.Printf("[supervisor] registry rejected %s: %s", service, resp.Error.Message)
		return
	}
	log.Printf("[supervisor] registered %s in registry", service)
}

// findServiceBinary locates a service binary by checking:
// 1. STRATA_<NAME>_BIN environment variable
// 2. Same directory as the supervisor binary
// 3. System PATH
func findServiceBinary(name string) (string, error) {
	envKey := "STRATA_" + strings.ToUpper(name) + "_BIN"
	if bin := os.Getenv(envKey); bin != "" {
		return bin, nil
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("binary %q not found (set STRATA_%s_BIN)", name, strings.ToUpper(name))
}
