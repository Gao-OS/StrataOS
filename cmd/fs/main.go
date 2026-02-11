// FS service: capability-protected filesystem access over UDS.
// Requires a valid PASETO token for every operation.
// Enforces path_prefix constraints from the capability.
package main

import (
	"crypto/ed25519"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Gao-OS/StrataOS/internal/auth"
	"github.com/Gao-OS/StrataOS/internal/capability"
	"github.com/Gao-OS/StrataOS/internal/ipc"
)

// handleTable maps opaque handle IDs to open file descriptors.
type handleTable struct {
	mu      sync.RWMutex
	handles map[string]*os.File
	nextID  atomic.Uint64
}

func newHandleTable() *handleTable {
	return &handleTable{handles: make(map[string]*os.File)}
}

func (ht *handleTable) Open(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	id := fmt.Sprintf("h%d", ht.nextID.Add(1))
	ht.mu.Lock()
	ht.handles[id] = f
	ht.mu.Unlock()
	return id, nil
}

func (ht *handleTable) Get(id string) (*os.File, bool) {
	ht.mu.RLock()
	defer ht.mu.RUnlock()
	f, ok := ht.handles[id]
	return f, ok
}

func (ht *handleTable) CloseAll() {
	ht.mu.Lock()
	defer ht.mu.Unlock()
	for _, f := range ht.handles {
		f.Close()
	}
	ht.handles = make(map[string]*os.File)
}

func main() {
	runtimeDir := os.Getenv("STRATA_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = "/run/strata"
	}

	log.Printf("[fs] starting")

	// Wait for identity service to publish its public key.
	pubKeyPath := filepath.Join(runtimeDir, "identity.pub")
	var pubKey ed25519.PublicKey
	for i := 0; i < 50; i++ {
		var err error
		pubKey, err = auth.LoadPublicKey(pubKeyPath)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if pubKey == nil {
		log.Fatalf("[fs] failed to load identity public key from %s", pubKeyPath)
	}
	log.Printf("[fs] loaded identity public key")

	handles := newHandleTable()

	verifyToken := func(req *ipc.Request, action string) (*capability.Capability, *ipc.Response) {
		if req.Auth == nil || req.Auth.Token == "" {
			resp := ipc.ErrorResponse(req.ReqID, ipc.ErrAuthRequired, "token required")
			return nil, &resp
		}
		cap, err := auth.Verify(req.Auth.Token, pubKey)
		if err != nil {
			resp := ipc.ErrorResponse(req.ReqID, ipc.ErrPermDenied, "invalid token: "+err.Error())
			return nil, &resp
		}
		if cap.IsExpired() {
			resp := ipc.ErrorResponse(req.ReqID, ipc.ErrPermDenied, "token expired")
			return nil, &resp
		}
		if cap.Service != "fs" {
			resp := ipc.ErrorResponse(req.ReqID, ipc.ErrPermDenied, "token not valid for fs service")
			return nil, &resp
		}
		if !cap.HasAction(action) {
			resp := ipc.ErrorResponse(req.ReqID, ipc.ErrPermDenied, fmt.Sprintf("action %q not permitted", action))
			return nil, &resp
		}
		return cap, nil
	}

	enforcePath := func(cap *capability.Capability, path string) error {
		if cap.Constraints.PathPrefix == "" {
			return nil
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("resolve path: %w", err)
		}
		prefix, err := filepath.Abs(cap.Constraints.PathPrefix)
		if err != nil {
			return fmt.Errorf("resolve prefix: %w", err)
		}
		// Ensure the path is within the allowed prefix (with trailing separator check).
		if absPath != prefix && !strings.HasPrefix(absPath, prefix+string(filepath.Separator)) {
			return fmt.Errorf("path %s outside allowed prefix %s", absPath, prefix)
		}
		return nil
	}

	srv := ipc.NewServer(filepath.Join(runtimeDir, "fs.sock"))

	srv.Handle("fs.open", func(req *ipc.Request) ipc.Response {
		cap, errResp := verifyToken(req, "open")
		if errResp != nil {
			return *errResp
		}
		path, _ := req.Params["path"].(string)
		if path == "" {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrInvalidRequest, "missing path param")
		}
		if err := enforcePath(cap, path); err != nil {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrPermDenied, err.Error())
		}
		handle, err := handles.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				return ipc.ErrorResponse(req.ReqID, ipc.ErrNotFound, "file not found")
			}
			return ipc.ErrorResponse(req.ReqID, ipc.ErrInternal, err.Error())
		}
		log.Printf("[fs] opened %s -> %s", path, handle)
		return ipc.SuccessResponse(req.ReqID, map[string]string{"handle": handle})
	})

	srv.Handle("fs.read", func(req *ipc.Request) ipc.Response {
		_, errResp := verifyToken(req, "read")
		if errResp != nil {
			return *errResp
		}
		handle, _ := req.Params["handle"].(string)
		if handle == "" {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrInvalidRequest, "missing handle param")
		}
		f, ok := handles.Get(handle)
		if !ok {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrNotFound, "invalid handle")
		}

		offset, _ := req.Params["offset"].(float64)
		size, _ := req.Params["size"].(float64)
		if size <= 0 {
			size = 4096
		}

		buf := make([]byte, int(size))
		n, err := f.ReadAt(buf, int64(offset))
		if err != nil && err != io.EOF {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrInternal, err.Error())
		}
		return ipc.SuccessResponse(req.ReqID, map[string]any{
			"data":       string(buf[:n]),
			"bytes_read": n,
		})
	})

	srv.Handle("fs.list", func(req *ipc.Request) ipc.Response {
		cap, errResp := verifyToken(req, "list")
		if errResp != nil {
			return *errResp
		}
		path, _ := req.Params["path"].(string)
		if path == "" {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrInvalidRequest, "missing path param")
		}
		if err := enforcePath(cap, path); err != nil {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrPermDenied, err.Error())
		}

		entries, err := os.ReadDir(path)
		if err != nil {
			if os.IsNotExist(err) {
				return ipc.ErrorResponse(req.ReqID, ipc.ErrNotFound, "directory not found")
			}
			return ipc.ErrorResponse(req.ReqID, ipc.ErrInternal, err.Error())
		}

		var items []map[string]any
		for _, e := range entries {
			item := map[string]any{
				"name":   e.Name(),
				"is_dir": e.IsDir(),
			}
			if info, err := e.Info(); err == nil {
				item["size"] = info.Size()
			}
			items = append(items, item)
		}
		return ipc.SuccessResponse(req.ReqID, map[string]any{"entries": items})
	})

	if err := srv.Start(); err != nil {
		log.Fatalf("[fs] start failed: %v", err)
	}
	log.Printf("[fs] ready")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Printf("[fs] shutting down")
	handles.CloseAll()
	srv.Stop()
}
