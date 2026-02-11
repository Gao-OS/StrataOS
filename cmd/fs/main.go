// FS service: capability-protected filesystem access over UDS.
// Requires a valid PASETO token for every operation.
// Authorization is delegated to the centralized policy layer.
// Handles are bound to the capability that opened them.
package main

import (
	"crypto/ed25519"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Gao-OS/StrataOS/internal/auth"
	"github.com/Gao-OS/StrataOS/internal/capability"
	"github.com/Gao-OS/StrataOS/internal/ipc"
	"github.com/Gao-OS/StrataOS/internal/policy"
)

// handleEntry binds an open file to the capability that opened it.
type handleEntry struct {
	file      *os.File
	capID     string
	path      string
	createdAt time.Time
}

// handleTable maps opaque handle IDs to open files and tracks revoked capabilities.
type handleTable struct {
	mu      sync.RWMutex
	handles map[string]*handleEntry
	revoked map[string]struct{}
	nextID  atomic.Uint64
}

func newHandleTable() *handleTable {
	return &handleTable{
		handles: make(map[string]*handleEntry),
		revoked: make(map[string]struct{}),
	}
}

func (ht *handleTable) Open(path, capID string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	absPath, _ := filepath.Abs(path)
	id := fmt.Sprintf("h%d", ht.nextID.Add(1))
	ht.mu.Lock()
	ht.handles[id] = &handleEntry{
		file:      f,
		capID:     capID,
		path:      absPath,
		createdAt: time.Now(),
	}
	ht.mu.Unlock()
	return id, nil
}

func (ht *handleTable) Get(id string) (*handleEntry, bool) {
	ht.mu.RLock()
	defer ht.mu.RUnlock()
	e, ok := ht.handles[id]
	return e, ok
}

func (ht *handleTable) Revoke(capID string) {
	ht.mu.Lock()
	defer ht.mu.Unlock()
	ht.revoked[capID] = struct{}{}
}

func (ht *handleTable) IsRevoked(capID string) bool {
	ht.mu.RLock()
	defer ht.mu.RUnlock()
	_, ok := ht.revoked[capID]
	return ok
}

func (ht *handleTable) CloseAll() {
	ht.mu.Lock()
	defer ht.mu.Unlock()
	for _, e := range ht.handles {
		e.file.Close()
	}
	ht.handles = make(map[string]*handleEntry)
}

// extractClaims verifies the PASETO token from the request.
// Returns nil claims if no token is present (policy.Authorize handles that).
// Returns an error response only if the token is present but cryptographically invalid.
func extractClaims(req *ipc.Request, pubKey ed25519.PublicKey) (*capability.Capability, *ipc.Response) {
	if req.Auth == nil || req.Auth.Token == "" {
		return nil, nil
	}
	cap, err := auth.Verify(req.Auth.Token, pubKey)
	if err != nil {
		resp := ipc.ErrorResponse(req.ReqID, ipc.ErrAuthRequired, "invalid token: "+err.Error())
		return nil, &resp
	}
	if cap.IsExpired() {
		resp := ipc.ErrorResponse(req.ReqID, ipc.ErrAuthRequired, "token expired")
		return nil, &resp
	}
	return cap, nil
}

// policyError converts a policy.PolicyError into an IPC error response.
func policyError(reqID string, err error) ipc.Response {
	if pe, ok := err.(*policy.PolicyError); ok {
		return ipc.ErrorResponse(reqID, pe.Code, pe.Message)
	}
	return ipc.ErrorResponse(reqID, ipc.ErrInternal, err.Error())
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
	srv := ipc.NewServer(filepath.Join(runtimeDir, "fs.sock"))

	srv.Handle("fs.open", func(req *ipc.Request) ipc.Response {
		claims, errResp := extractClaims(req, pubKey)
		if errResp != nil {
			return *errResp
		}

		path, _ := req.Params["path"].(string)
		if path == "" {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrInvalidRequest, "missing path param")
		}

		if err := policy.Authorize(claims, "fs.open", map[string]any{"path": path}); err != nil {
			return policyError(req.ReqID, err)
		}

		if handles.IsRevoked(claims.ID) {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrPermDenied, "capability revoked")
		}

		handle, err := handles.Open(path, claims.ID)
		if err != nil {
			if os.IsNotExist(err) {
				return ipc.ErrorResponse(req.ReqID, ipc.ErrNotFound, "file not found")
			}
			return ipc.ErrorResponse(req.ReqID, ipc.ErrInternal, err.Error())
		}
		log.Printf("[fs] opened %s -> %s (cap=%s)", path, handle, claims.ID)
		return ipc.SuccessResponse(req.ReqID, map[string]string{"handle": handle})
	})

	srv.Handle("fs.read", func(req *ipc.Request) ipc.Response {
		claims, errResp := extractClaims(req, pubKey)
		if errResp != nil {
			return *errResp
		}

		// No path context for read — handle was already opened with permission.
		if err := policy.Authorize(claims, "fs.read", nil); err != nil {
			return policyError(req.ReqID, err)
		}

		handle, _ := req.Params["handle"].(string)
		if handle == "" {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrInvalidRequest, "missing handle param")
		}
		entry, ok := handles.Get(handle)
		if !ok {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrNotFound, "invalid handle")
		}

		// Handle binding: only the capability that opened the handle may use it.
		if entry.capID != claims.ID {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrPermDenied, "handle not bound to this capability")
		}

		if handles.IsRevoked(entry.capID) {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrPermDenied, "capability revoked")
		}

		offset, _ := req.Params["offset"].(float64)
		size, _ := req.Params["size"].(float64)
		if size <= 0 {
			size = 4096
		}

		buf := make([]byte, int(size))
		n, err := entry.file.ReadAt(buf, int64(offset))
		if err != nil && err != io.EOF {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrInternal, err.Error())
		}
		return ipc.SuccessResponse(req.ReqID, map[string]any{
			"data":       string(buf[:n]),
			"bytes_read": n,
		})
	})

	srv.Handle("fs.list", func(req *ipc.Request) ipc.Response {
		claims, errResp := extractClaims(req, pubKey)
		if errResp != nil {
			return *errResp
		}

		path, _ := req.Params["path"].(string)
		if path == "" {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrInvalidRequest, "missing path param")
		}

		if err := policy.Authorize(claims, "fs.list", map[string]any{"path": path}); err != nil {
			return policyError(req.ReqID, err)
		}

		if handles.IsRevoked(claims.ID) {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrPermDenied, "capability revoked")
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

	// Internal revocation notification from identity service.
	srv.Handle("fs.revoke", func(req *ipc.Request) ipc.Response {
		capID, _ := req.Params["cap_id"].(string)
		if capID == "" {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrInvalidRequest, "missing cap_id param")
		}
		handles.Revoke(capID)
		log.Printf("[fs] capability %s revoked (handles invalidated)", capID)
		return ipc.SuccessResponse(req.ReqID, map[string]string{"status": "revoked"})
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
