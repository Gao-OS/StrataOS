// strata-ctl: command-line client for Strata services.
// Sends a single IPC request and prints the JSON response.
//
// Usage:
//   strata-ctl <method> [params_json]
//   strata-ctl -token <TOKEN> <method> [params_json]
//
// The target socket is inferred from the method prefix:
//   identity.* → identity.sock
//   fs.*       → fs.sock
//   supervisor.* → supervisor.sock
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

	// Derive socket path from method prefix (e.g. "fs.open" → "fs.sock").
	service := strings.SplitN(method, ".", 2)[0]
	socketPath := filepath.Join(runtimeDir, service+".sock")

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
