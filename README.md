# Strata

A NixOS-native, capability-oriented distributed runtime substrate.

Strata implements a logical microkernel/exokernel-style control layer on top of Linux
namespaces, cgroups, and systemd. It is **not** a Linux replacement, not an AI orchestration
system, and not a monolithic application. It is a distributed capability-based service runtime.

**Version:** 0.3.0-mvp1

## Architecture

```
┌─────────────────────────────────────┐
│            Supervisor               │
│  (lifecycle, control socket)        │
│                                     │
│  ┌──────────┐    ┌──────────┐       │
│  │ Identity  │    │    FS    │       │
│  │ (tokens)  │    │ (files)  │       │
│  └────┬─────┘    └────┬─────┘       │
│       │               │             │
│  identity.sock     fs.sock          │
└───────┴───────────────┴─────────────┘
        Unix Domain Sockets
        Length-prefixed JSON
```

- **Supervisor** starts and manages identity and fs as child processes
- **Identity** generates an ed25519 keypair and issues PASETO v2.public capability tokens
- **FS** provides capability-gated filesystem operations (open, read, list)
- All IPC is local via Unix domain sockets with length-prefixed JSON framing

## Build

### With Nix

```sh
nix build .#supervisor
nix build .#identity
nix build .#fs
nix build .#strata-ctl
```

### With Go

```sh
go build -o ./bin/ ./cmd/...
```

Produces: `./bin/supervisor`, `./bin/identity`, `./bin/fs`, `./bin/strata-ctl`

### Development Shell

```sh
# Via Nix flake
nix develop

# Via devenv
devenv shell
strata-build   # builds all binaries to ./bin/
strata-run     # starts the supervisor
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

The supervisor finds `identity` and `fs` binaries in the same directory as itself.
Override with `STRATA_IDENTITY_BIN` and `STRATA_FS_BIN` environment variables.

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

## Development

```sh
# Enter dev shell
nix develop          # via flake
devenv shell         # via devenv

# Build all binaries
go build -o ./bin/ ./cmd/...

# Start supervisor (in one terminal)
export STRATA_RUNTIME_DIR=/tmp/strata
mkdir -p "$STRATA_RUNTIME_DIR"
./bin/supervisor

# Run smoke test (in another terminal)
sh scripts/smoke.sh
```

See [docs/EXECUTION.md](docs/EXECUTION.md) for iteration workflow and review checklists.

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
  };
}
```

## Protocol

See [api/protocol.md](api/protocol.md) for the full IPC protocol specification.

## Project Structure

```
cmd/
  supervisor/    Node-local process supervisor
  identity/      Capability token issuer (PASETO v2.public)
  fs/            Capability-gated filesystem service
  strata-ctl/    CLI client for interacting with services
internal/
  ipc/           Length-prefixed JSON framing and UDS server
  auth/          Ed25519 keys, PASETO signing/verification, revocation
  capability/    Token claims and constraint types
modules/
  strata.nix     NixOS module scaffold
api/
  protocol.md    IPC protocol specification
```

## Tech Stack

| Component       | Technology               |
|-----------------|--------------------------|
| Services        | Go (stdlib only)         |
| IPC             | Unix domain sockets      |
| Protocol        | Length-prefixed JSON      |
| Tokens          | PASETO v2.public (ed25519) |
| Build           | Nix flakes               |
| Dev             | devenv                   |
| Future control  | Elixir (not yet implemented) |
