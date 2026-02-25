# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Strata is a NixOS-native, capability-oriented distributed runtime substrate. It implements a logical microkernel/exokernel-style control layer on top of Linux namespaces, cgroups, and systemd. **Not** an AI system, not a Linux replacement — a distributed capability-based service runtime.

Current version: **v0.3.0-mvp1** (node-local only, no cluster logic).

## Build & Run

```sh
# Build all binaries
go build -o ./bin/ ./cmd/...

# Run (starts supervisor → identity → fs)
export STRATA_RUNTIME_DIR=/tmp/strata
mkdir -p $STRATA_RUNTIME_DIR
./bin/supervisor

# Nix build individual packages
nix build .#supervisor
nix build .#identity
nix build .#fs
nix build .#strata-ctl

# NixOS images
nix build .#nixosConfigurations.strata-iso-x86_64.config.system.build.isoImage
nix build .#nixosConfigurations.strata-vm-x86_64.config.system.build.vm

# Dev shell
nix develop        # flake-based
devenv shell       # devenv-based (then: strata-build, strata-run, strata-clean)
```

## Testing

```sh
# Run all unit tests
go test ./internal/...

# Run with race detector
go test -race ./internal/...

# Verbose output
go test -v ./internal/...

# Run a specific package
go test ./internal/policy/

# Integration smoke test (start supervisor first)
sh scripts/smoke.sh
```

## Go Tooling

Available via `devenv shell`:

```sh
# LSP (editor integration)
gopls                          # Go language server

# Linting
golangci-lint run              # aggregated linter (vet, errcheck, staticcheck, etc.)
staticcheck ./...              # standalone static analysis

# Formatting
goimports -w .                 # gofmt + auto-manage imports
go fmt ./...                   # standard formatting

# Debugging
dlv debug ./cmd/supervisor     # interactive debugger
dlv test ./internal/policy     # debug tests

# Vetting
go vet ./...                   # built-in static checks
```

## Architecture

Three services managed by a supervisor, communicating over Unix domain sockets with length-prefixed JSON (4-byte big-endian header + JSON payload, max 1 MiB frame):

- **Supervisor** (`cmd/supervisor/`) — starts identity and fs as child processes, exposes stub control socket, handles graceful shutdown (SIGINT/SIGTERM)
- **Identity** (`cmd/identity/`) — generates ed25519 keypair at startup, issues PASETO v2.public capability tokens, maintains in-memory revocation list, propagates revocations to FS (fire-and-forget)
- **FS** (`cmd/fs/`) — capability-gated file operations (open/read/list), verifies tokens locally using identity's published public key, maintains handle table bound to cap_ids, enforces `path_prefix` constraints via centralized policy
- **strata-ctl** (`cmd/strata-ctl/`) — CLI client, infers target socket from method prefix (e.g., `fs.open` → `fs.sock`)

### Startup sequence

Supervisor → starts identity → waits for `identity.pub` file → starts fs (which loads the public key) → opens control socket.

### Key Internal Packages

- `internal/ipc` — UDS server with `Handler` func type, length-prefixed framing, `SendRequest` client helper
- `internal/auth` — ed25519 key generation, PASETO v2.public sign/verify (stdlib-only PAE implementation), `RevocationList` (thread-safe, in-memory)
- `internal/capability` — `Capability` struct with `Rights` (preferred, fully-qualified like `fs.open`) and `Actions` (legacy fallback), `Constraints` (path_prefix, rate_limit)
- `internal/policy` — `Authorize(claims, method, ctx)` is the single authorization checkpoint; enforces service match, rights/actions, path prefix (with traversal protection), rate limiting (token bucket per cap_id)

### Authorization Flow

Every protected handler must call `policy.Authorize(claims, method, ctx)` — deny-by-default. The `ctx` map passes method-specific data (e.g., `{"path": "/tmp/foo"}` for path prefix enforcement). FS handle operations also check cap_id binding and revocation status on every access.

## IPC Protocol

Request: `{"v":1, "req_id":"...", "method":"service.action", "auth":{"token":"..."}, "params":{}}`
Response: `{"v":1, "req_id":"...", "ok":bool, "result":{}, "error":{"code":N, "name":"...", "message":"..."}}`

Error codes: 1=INVALID_ARGUMENT, 2=UNAUTHENTICATED, 3=PERMISSION_DENIED, 4=NOT_FOUND, 5=INTERNAL, 6=UNAVAILABLE, 7=RESOURCE_EXHAUSTED, 8=CONFLICT.
Full spec: `api/protocol.md`

## Runtime Layout

All sockets and state under `STRATA_RUNTIME_DIR` (default `/run/strata`, use `/tmp/strata` for dev):
- `supervisor.sock`, `identity.sock`, `fs.sock` — IPC sockets
- `identity.pub` — base64-encoded ed25519 public key (written by identity, read by fs)

## Conventions

- Go stdlib only — no external dependencies (`vendorHash = null` in Nix)
- No global state; config via environment variables (`STRATA_RUNTIME_DIR`, `STRATA_IDENTITY_BIN`, `STRATA_FS_BIN`, `STRATA_NODE_ID`)
- No persistent state at MVP — all in-memory, lost on restart
- `cmd/` for binaries, `internal/` for shared libraries
- PASETO v2.public over JWT (prevents algorithm confusion attacks)
- Log with `[service-name]` prefix (e.g., `[fs]`, `[identity]`)
- Use `sync.RWMutex` for concurrent state (handle table, revocation list, rate limiter)
- Update `api/protocol.md` first when adding new IPC methods
- Use fully-qualified rights (`fs.open`) over legacy actions (`open`)
- Naming: packages lowercase single word, binaries hyphenated, methods `service.action`, errors `UPPER_SNAKE`, sockets `service.sock`, env vars `STRATA_UPPER`, cap IDs hex
- NixOS module scaffold at `modules/strata.nix` (not fully implemented)
- Elixir control plane is planned but not yet implemented

## Roadmap Reference

See `docs/PLAN.md` for the full roadmap. Current position: v0.3.1 (policy layer) mostly complete. Next: v0.3.2 (supervisor state machine, registry service), v0.3.3 (NixOS activation), v0.4 (distributed hooks).

Known issues tracked in `docs/POLICY_REVIEW.md`: rate limiter memory leak, missing unit tests, fs.revoke lacks caller authentication, audit logging not implemented.
