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

# Nix build
nix build .#supervisor
nix build .#identity
nix build .#fs
nix build .#strata-ctl

# Dev shell
nix develop        # flake-based
devenv shell       # devenv-based (then: strata-build, strata-run)
```

## Architecture

Three services managed by a supervisor, communicating over Unix domain sockets with length-prefixed JSON:

- **Supervisor** (`cmd/supervisor/`) — starts identity and fs as child processes, exposes stub control socket
- **Identity** (`cmd/identity/`) — generates ed25519 keypair at startup, issues PASETO v2.public capability tokens, maintains in-memory revocation list
- **FS** (`cmd/fs/`) — capability-gated file operations (open/read/list), verifies tokens using identity's published public key, enforces `path_prefix` constraints, server-side file handle table
- **strata-ctl** (`cmd/strata-ctl/`) — CLI client, infers target socket from method prefix

## Key Internal Packages

- `internal/ipc` — UDS server, length-prefixed framing (4-byte BE header + JSON), request/response types, `SendRequest` client helper
- `internal/auth` — ed25519 key generation, PASETO v2.public sign/verify (stdlib-only implementation using PAE), revocation list
- `internal/capability` — capability token claims (service, actions, constraints with path_prefix)

## IPC Protocol

Request: `{"v":1, "req_id":"...", "method":"service.action", "auth":{"token":"..."}, "params":{}}`
Response: `{"v":1, "req_id":"...", "ok":bool, "result":{}, "error":{"code":N, "message":"..."}}`

Error codes: 1=invalid request, 2=auth required, 3=permission denied, 4=not found, 5=internal.
Full spec: `api/protocol.md`

## Runtime Layout

All sockets and state under `STRATA_RUNTIME_DIR` (default `/run/strata`, use `/tmp/strata` for dev):
- `supervisor.sock`, `identity.sock`, `fs.sock` — IPC sockets
- `identity.pub` — base64-encoded ed25519 public key (written by identity, read by fs)

## Conventions

- Go stdlib only — no external dependencies
- No global state; services receive config via environment variables (`STRATA_RUNTIME_DIR`, `STRATA_IDENTITY_BIN`, `STRATA_FS_BIN`)
- Clear package boundaries: `cmd/` for binaries, `internal/` for shared libraries
- PASETO v2.public implemented directly (no external crypto libs beyond Go stdlib)
- Nix flakes for reproducible builds, devenv for development
- NixOS module scaffold at `modules/strata.nix` (not fully implemented)
- Elixir control plane is planned but not yet implemented
