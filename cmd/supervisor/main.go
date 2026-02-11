// Supervisor: top-level process that manages the Strata service lifecycle.
// Starts identity and fs as child processes, creates the runtime directory,
// and exposes a stub control socket.
package main

import (
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
)

func main() {
	runtimeDir := os.Getenv("STRATA_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = "/run/strata"
	}

	log.Printf("[supervisor] starting (runtime_dir=%s)", runtimeDir)

	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		log.Fatalf("[supervisor] create runtime dir: %v", err)
	}

	// Start identity first — it publishes its public key for other services.
	identityBin, err := findServiceBinary("identity")
	if err != nil {
		log.Fatalf("[supervisor] %v", err)
	}
	identityCmd := startService("identity", identityBin, runtimeDir)

	identitySock := filepath.Join(runtimeDir, "identity.sock")
	if !waitForFile(identitySock, 5*time.Second) {
		log.Fatalf("[supervisor] identity service did not start (waiting for %s)", identitySock)
	}
	log.Printf("[supervisor] identity service ready")

	// Start fs after identity's public key is available.
	fsBin, err := findServiceBinary("fs")
	if err != nil {
		log.Fatalf("[supervisor] %v", err)
	}
	fsCmd := startService("fs", fsBin, runtimeDir)

	fsSock := filepath.Join(runtimeDir, "fs.sock")
	if !waitForFile(fsSock, 5*time.Second) {
		log.Fatalf("[supervisor] fs service did not start (waiting for %s)", fsSock)
	}
	log.Printf("[supervisor] fs service ready")

	// Stub control socket.
	ctlSrv := ipc.NewServer(filepath.Join(runtimeDir, "supervisor.sock"))
	ctlSrv.Handle("supervisor.status", func(req *ipc.Request) ipc.Response {
		return ipc.SuccessResponse(req.ReqID, map[string]string{
			"status":   "running",
			"identity": "running",
			"fs":       "running",
		})
	})
	if err := ctlSrv.Start(); err != nil {
		log.Fatalf("[supervisor] control socket: %v", err)
	}

	log.Printf("[supervisor] all services running")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Printf("[supervisor] shutting down")
	ctlSrv.Stop()
	stopService(fsCmd, "fs")
	stopService(identityCmd, "identity")
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

func startService(name, bin, runtimeDir string) *exec.Cmd {
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), fmt.Sprintf("STRATA_RUNTIME_DIR=%s", runtimeDir))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Fatalf("[supervisor] start %s: %v", name, err)
	}
	log.Printf("[supervisor] started %s (pid=%d)", name, cmd.Process.Pid)
	return cmd
}

func stopService(cmd *exec.Cmd, name string) {
	if cmd.Process != nil {
		log.Printf("[supervisor] stopping %s (pid=%d)", name, cmd.Process.Pid)
		cmd.Process.Signal(syscall.SIGTERM)
		cmd.Wait()
	}
}

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
