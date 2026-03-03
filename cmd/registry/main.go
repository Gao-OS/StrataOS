// Registry service: in-memory service endpoint registry over UDS.
// Services register their endpoints; clients resolve them by name.
package main

import (
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Gao-OS/StrataOS/internal/ipc"
	"github.com/Gao-OS/StrataOS/internal/registry"
)

func main() {
	runtimeDir := os.Getenv("STRATA_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = "/run/strata"
	}

	log.Printf("[registry] starting")

	reg := registry.New()

	srv := ipc.NewServer(filepath.Join(runtimeDir, "registry.sock"))

	srv.Handle("registry.register", func(req *ipc.Request) ipc.Response {
		service, _ := req.Params["service"].(string)
		endpoint, _ := req.Params["endpoint"].(string)
		apiv, _ := req.Params["api_v"].(float64) // JSON numbers are float64

		if service == "" {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrInvalidRequest, "missing service param")
		}
		if endpoint == "" {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrInvalidRequest, "missing endpoint param")
		}
		if apiv == 0 {
			apiv = 1
		}

		reg.Register(service, endpoint, int(apiv))
		log.Printf("[registry] registered %s -> %s (api_v=%d)", service, endpoint, int(apiv))
		return ipc.SuccessResponse(req.ReqID, map[string]string{"status": "registered"})
	})

	srv.Handle("registry.resolve", func(req *ipc.Request) ipc.Response {
		service, _ := req.Params["service"].(string)
		if service == "" {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrInvalidRequest, "missing service param")
		}
		entry, ok := reg.Resolve(service)
		if !ok {
			return ipc.ErrorResponse(req.ReqID, ipc.ErrNotFound, "service not registered: "+service)
		}
		return ipc.SuccessResponse(req.ReqID, map[string]any{
			"endpoint": entry.Endpoint,
			"api_v":    entry.APIv,
		})
	})

	srv.Handle("registry.list", func(req *ipc.Request) ipc.Response {
		entries := reg.List()
		return ipc.SuccessResponse(req.ReqID, map[string]any{
			"services": entries,
		})
	})

	if err := srv.Start(); err != nil {
		log.Fatalf("[registry] %v", err)
	}
	log.Printf("[registry] ready")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Printf("[registry] shutting down")
	srv.Stop()
}
