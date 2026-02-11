// Identity service: generates ed25519 keypair, issues and revokes
// PASETO v2.public capability tokens over UDS.
package main

import (
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Gao-OS/StrataOS/internal/auth"
	"github.com/Gao-OS/StrataOS/internal/capability"
	"github.com/Gao-OS/StrataOS/internal/ipc"
)

func main() {
	runtimeDir := os.Getenv("STRATA_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = "/run/strata"
	}

	log.Printf("[identity] starting")

	kp, err := auth.GenerateKeyPair()
	if err != nil {
		log.Fatalf("[identity] keypair generation failed: %v", err)
	}

	// Publish public key so other services can verify tokens locally.
	pubKeyPath := filepath.Join(runtimeDir, "identity.pub")
	if err := kp.WritePublicKey(pubKeyPath); err != nil {
		log.Fatalf("[identity] write public key: %v", err)
	}
	log.Printf("[identity] public key written to %s", pubKeyPath)

	revocations := auth.NewRevocationList()

	srv := ipc.NewServer(filepath.Join(runtimeDir, "identity.sock"))

	srv.Handle("identity.issue", func(req *ipc.Request) ipc.Response {
		service, _ := req.Params["service"].(string)
		if service == "" {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrInvalidRequest, "missing service param")
		}

		var actions []string
		if raw, ok := req.Params["actions"].([]any); ok {
			for _, a := range raw {
				if s, ok := a.(string); ok {
					actions = append(actions, s)
				}
			}
		}
		var rights []string
		if raw, ok := req.Params["rights"].([]any); ok {
			for _, r := range raw {
				if s, ok := r.(string); ok {
					rights = append(rights, s)
				}
			}
		}

		if len(actions) == 0 && len(rights) == 0 {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrInvalidRequest, "actions or rights required")
		}

		pathPrefix, _ := req.Params["path_prefix"].(string)
		rateLimit, _ := req.Params["rate_limit"].(string)

		ttlSec, _ := req.Params["ttl_seconds"].(float64)
		if ttlSec <= 0 {
			ttlSec = 3600
		}

		cap := capability.NewCapability(service, actions, capability.Constraints{
			PathPrefix: pathPrefix,
			RateLimit:  rateLimit,
		}, time.Duration(ttlSec)*time.Second)
		cap.Rights = rights

		token, err := auth.Sign(cap, kp.Private)
		if err != nil {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrInternal, err.Error())
		}

		log.Printf("[identity] issued capability %s for service=%s actions=%v prefix=%q",
			cap.ID, service, actions, pathPrefix)

		return ipc.SuccessResponse(req.ReqID, map[string]any{
			"token":   token,
			"cap_id":  cap.ID,
			"expires": cap.ExpiresAt.Unix(),
		})
	})

	srv.Handle("identity.revoke", func(req *ipc.Request) ipc.Response {
		capID, _ := req.Params["cap_id"].(string)
		if capID == "" {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrInvalidRequest, "missing cap_id param")
		}
		revocations.Revoke(capID)
		log.Printf("[identity] revoked capability %s", capID)

		// Notify FS to invalidate handles bound to this capability.
		fsSock := filepath.Join(runtimeDir, "fs.sock")
		if _, err := ipc.SendRequest(fsSock, &ipc.Request{
			V:      1,
			ReqID:  "revoke-" + capID,
			Method: "fs.revoke",
			Params: map[string]any{"cap_id": capID},
		}); err != nil {
			log.Printf("[identity] fs revocation notify failed: %v", err)
		}

		return ipc.SuccessResponse(req.ReqID, map[string]string{"status": "revoked"})
	})

	if err := srv.Start(); err != nil {
		log.Fatalf("[identity] start failed: %v", err)
	}
	log.Printf("[identity] ready")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Printf("[identity] shutting down")
	srv.Stop()
}
