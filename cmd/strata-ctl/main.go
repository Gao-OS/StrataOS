// strata-ctl: command-line client for Strata services.
// Sends a single IPC request and prints the JSON response.
//
// Usage:
//
//	strata-ctl <method> [params_json]
//	strata-ctl -token <TOKEN> <method> [params_json]
//
// The target socket is resolved via the registry service when available,
// with fallback to the convention: method prefix → service.sock.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/Gao-OS/StrataOS/internal/ipc"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: strata-ctl [-token TOKEN] <method> [params_json]\n")
		os.Exit(1)
	}

	runtimeDir := os.Getenv("STRATA_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = "/run/strata"
	}

	args := os.Args[1:]
	var token string

	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "-token":
			if len(args) < 2 {
				fmt.Fprintf(os.Stderr, "error: missing token value\n")
				os.Exit(1)
			}
			token = args[1]
			args = args[2:]
		default:
			fmt.Fprintf(os.Stderr, "error: unknown flag %s\n", args[0])
			os.Exit(1)
		}
	}

	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "error: missing method\n")
		os.Exit(1)
	}

	method := args[0]
	var params map[string]any
	if len(args) > 1 {
		if err := json.Unmarshal([]byte(args[1]), &params); err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid params JSON: %v\n", err)
			os.Exit(1)
		}
	}

	socketPath := resolveSocket(runtimeDir, method)

	idBytes := make([]byte, 8)
	rand.Read(idBytes)

	req := &ipc.Request{
		V:      1,
		ReqID:  hex.EncodeToString(idBytes),
		Method: method,
		Params: params,
	}
	if token != "" {
		req.Auth = &ipc.Auth{Token: token}
	}

	resp, err := ipc.SendRequest(socketPath, req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	out, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Println(string(out))

	if !resp.OK {
		os.Exit(1)
	}
}

// resolveSocket determines the target socket for a method.
// For registry.* and supervisor.* methods, uses direct convention (can't resolve themselves).
// For other methods, tries registry.resolve first, then falls back to convention.
func resolveSocket(runtimeDir, method string) string {
	service := strings.SplitN(method, ".", 2)[0]

	// Self-referential services can't use the registry to resolve themselves.
	if service == "registry" || service == "supervisor" {
		return filepath.Join(runtimeDir, service+".sock")
	}

	// Try registry resolution.
	registrySock := filepath.Join(runtimeDir, "registry.sock")
	endpoint, err := registryResolve(registrySock, service)
	if err == nil && endpoint != "" {
		// Parse "unix:///path" → "/path"
		if strings.HasPrefix(endpoint, "unix://") {
			return strings.TrimPrefix(endpoint, "unix://")
		}
		return endpoint
	}

	// Fallback to socket convention.
	log.Printf("[strata-ctl] registry unavailable, using convention for %s", service)
	return filepath.Join(runtimeDir, service+".sock")
}

// registryResolve calls registry.resolve and returns the endpoint.
func registryResolve(registrySock, service string) (string, error) {
	req := &ipc.Request{
		V:      1,
		ReqID:  "resolve-" + service,
		Method: "registry.resolve",
		Params: map[string]any{"service": service},
	}
	resp, err := ipc.SendRequest(registrySock, req)
	if err != nil {
		return "", err
	}
	if !resp.OK {
		return "", fmt.Errorf("resolve failed: %s", resp.Error.Message)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		return "", fmt.Errorf("unexpected result type")
	}
	endpoint, _ := result["endpoint"].(string)
	return endpoint, nil
}
