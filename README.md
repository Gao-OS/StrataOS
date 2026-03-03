# Strata

A NixOS-native, capability-oriented distributed runtime substrate.

Strata implements a logical microkernel/exokernel-style control layer on top of Linux
namespaces, cgroups, and systemd. It is **not** a Linux replacement, not an AI orchestration
system, and not a monolithic application. It is a distributed capability-based service runtime.

**Version:** 0.3.2

## Architecture

```
┌───────────────────────────────────────────────┐
│                 Supervisor                     │
│  (Manager state machine, crash recovery,      │
│   backoff, quarantine, control socket)         │
│                                               │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐    │
│  │ Registry │  │ Identity │  │    FS    │    │
│  │(discover)│  │ (tokens) │  │ (files)  │    │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘    │
│       │              │             │           │
│  registry.sock  identity.sock   fs.sock       │
└───────┴──────────────┴─────────────┴───────────┘
          Unix Domain Sockets
          Length-prefixed JSON
```

- **Supervisor** manages all services via a state machine with dependency-ordered startup (topological sort), exponential backoff crash recovery, and sliding window quarantine. Exposes `supervisor.status`, `supervisor.svc.list`, `supervisor.svc.start`, `supervisor.svc.stop`
- **Registry** provides in-memory service endpoint discovery (`registry.register`, `registry.resolve`, `registry.list`). No auth required (socket-level trust)
- **Identity** generates an ed25519 keypair, issues PASETO v2.public capability tokens, maintains an in-memory revocation list
- **FS** provides capability-gated filesystem operations (open, read, list), verifies tokens locally using identity's public key, enforces path prefix constraints via centralized policy
- **strata-ctl** is the CLI client; resolves target sockets via registry with fallback to convention

### Service Lifecycle States

`Declared` → `Starting` → `Healthy` → `Crashed` → `Restarting` → (back to Starting)

A service that crashes too many times within a window is `Quarantined` and requires manual restart. `Stopped` services can be restarted via `supervisor.svc.start`.

### Startup Sequence

Supervisor → starts registry (no deps) → starts identity (no deps) → waits for `identity.pub` → starts fs (depends on identity) → opens control socket. Healthy services are auto-registered in registry.

### Authorization Model

All protected handlers call `policy.Authorize(claims, method, ctx)` — deny-by-default. Tokens are service-scoped and carry fully-qualified rights (e.g., `fs.open`). FS handles are bound to the cap_id that opened them and checked for revocation on every access.

## Build

### Individual Binaries

#### With Nix

```sh
nix build .#supervisor
nix build .#identity
nix build .#fs
nix build .#registry
nix build .#strata-ctl
```

#### With Go

```sh
go build -o ./bin/ ./cmd/...
```

Produces: `./bin/supervisor`, `./bin/identity`, `./bin/fs`, `./bin/registry`, `./bin/strata-ctl`

### OS Images

Build bootable NixOS images with Strata pre-configured:

```sh
# ISO image (live environment, x86_64)
nix build .#nixosConfigurations.strata-iso-x86_64.config.system.build.isoImage
# Output: ./result/iso/strata-*.iso

# QEMU VM image (x86_64)
nix build .#nixosConfigurations.strata-vm-x86_64.config.system.build.vm
# Output: ./result/bin/run-strata-vm
# Run: ./result/bin/run-strata-vm

# Boot the ISO in QEMU
qemu-system-x86_64 -cdrom ./result/iso/strata-*.iso -m 2G
```

### Development Shell

```sh
# Via Nix flake
nix develop

# Via devenv
devenv shell
strata-build   # builds all binaries to ./bin/
strata-run     # starts the supervisor
strata-clean   # removes ./bin and /tmp/strata
```

## Run

```sh
# Build first
go build -o ./bin/ ./cmd/...

# Start the supervisor (creates runtime dir, starts all services)
export STRATA_RUNTIME_DIR=/tmp/strata
mkdir -p $STRATA_RUNTIME_DIR
./bin/supervisor
```

The supervisor finds `registry`, `identity`, and `fs` binaries in the same directory as itself.
Override with `STRATA_REGISTRY_BIN`, `STRATA_IDENTITY_BIN`, and `STRATA_FS_BIN` environment variables.

## Usage Examples

With the supervisor running in one terminal:

### 1. Request a capability token

```sh
export STRATA_RUNTIME_DIR=/tmp/strata

./bin/strata-ctl identity.issue \
  '{"service":"fs","actions":["open","read","list"],"path_prefix":"/tmp"}'
```

Response:

```json
{
  "v": 1,
  "req_id": "...",
  "ok": true,
  "result": {
    "token": "v2.public.eyJq...",
    "cap_id": "abc123...",
    "expires": 1700000000
  }
}
```

### 2. List a directory using the token

```sh
TOKEN="v2.public.eyJq..."   # from step 1

./bin/strata-ctl -token "$TOKEN" fs.list '{"path":"/tmp"}'
```

### 3. Open and read a file

```sh
# Create a test file
echo "hello strata" > /tmp/test.txt

# Open it
./bin/strata-ctl -token "$TOKEN" fs.open '{"path":"/tmp/test.txt"}'
# → {"handle": "h1"}

# Read from the handle
./bin/strata-ctl -token "$TOKEN" fs.read '{"handle":"h1","offset":0,"size":4096}'
# → {"data": "hello strata\n", "bytes_read": 13}
```

### 4. Check supervisor status

```sh
./bin/strata-ctl supervisor.status
```

### 5. List managed services

```sh
./bin/strata-ctl supervisor.svc.list
```

### 6. Resolve a service via registry

```sh
./bin/strata-ctl registry.resolve '{"service":"fs"}'
```

### 7. List all registered services

```sh
./bin/strata-ctl registry.list
```

## Testing

```sh
# Run unit tests
go test ./internal/...

# Run with race detector
go test -race ./internal/...

# Run smoke test (with supervisor running in another terminal)
sh scripts/smoke.sh
```

## NixOS Module

A scaffold NixOS module is provided at `modules/strata.nix`:

```nix
{
  imports = [ strataOS.nixosModules.strata ];

  services.strata = {
    enable = true;
    nodeId = "node-1";
    package = strataOS.packages.x86_64-linux.supervisor;
    identityPackage = strataOS.packages.x86_64-linux.identity;
    fsPackage = strataOS.packages.x86_64-linux.fs;
    registryPackage = strataOS.packages.x86_64-linux.registry;
  };
}
```

## Protocol

See [api/protocol.md](api/protocol.md) for the full IPC protocol specification.

Error codes:

| Code | Name                | Meaning                    |
|------|---------------------|----------------------------|
| 1    | INVALID_ARGUMENT    | Malformed or missing param |
| 2    | UNAUTHENTICATED     | Token missing or invalid   |
| 3    | PERMISSION_DENIED   | Insufficient rights        |
| 4    | NOT_FOUND           | Resource does not exist    |
| 5    | INTERNAL            | Unexpected server error    |
| 6    | UNAVAILABLE         | Service not reachable      |
| 7    | RESOURCE_EXHAUSTED  | Rate limit exceeded        |
| 8    | CONFLICT            | State conflict             |

## Project Structure

```
cmd/
  supervisor/    Node-local process supervisor (Manager state machine)
  registry/      In-memory service endpoint registry
  identity/      Capability token issuer (PASETO v2.public)
  fs/            Capability-gated filesystem service
  strata-ctl/    CLI client for interacting with services
internal/
  ipc/           Length-prefixed JSON framing and UDS server
  auth/          Ed25519 keys, PASETO signing/verification, revocation
  capability/    Token claims and constraint types
  policy/        Centralized authorization and constraint enforcement
  supervisor/    Service lifecycle state machine, backoff, quarantine, Manager
  registry/      Thread-safe in-memory service registry
modules/
  strata.nix     NixOS module scaffold
api/
  protocol.md    IPC protocol specification
docs/
  PLAN.md        Roadmap (v0.3 → v0.4)
  EXECUTION.md   Iteration workflow and review checklists
  POLICY_REVIEW.md  Security review and known issues
scripts/
  smoke.sh       Integration smoke test
```

## Tech Stack

| Component       | Technology               |
|-----------------|--------------------------|
| Services        | Go (stdlib only)         |
| IPC             | Unix domain sockets      |
| Protocol        | Length-prefixed JSON      |
| Tokens          | PASETO v2.public (ed25519) |
| Build           | Nix flakes               |
| OS Images       | NixOS configurations     |
| Dev             | devenv                   |
| Future control  | Elixir (not yet implemented) |
