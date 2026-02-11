# Strata Execution Guide

This document describes how we develop Strata step-by-step without losing control.

---

## Principles

1. **One step per iteration**
   - Implement one planned step.
   - Build + run + smoke test.
   - Commit.
2. **Keep protocol stable**
   - Version field remains `v=1`.
   - Additive changes only.
3. **Centralize security decisions**
   - All authorization must go through `internal/policy`.
4. **Keep services small and replaceable**
   - No hidden global state.
   - Clear interfaces.

---

## Repository Conventions

Recommended structure (current baseline):

- `cmd/*`: binaries
- `internal/*`: shared libraries (ipc/auth/capability/policy/service/registry)
- `api/protocol.md`: protocol source of truth
- `modules/strata.nix`: NixOS module scaffold
- `flake.nix`, `devenv.nix`: build/dev
- `docs/`: project plans, execution guide, implementation notes
- `scripts/`: smoke tests and development scripts

---

## Development Environment

### devenv
- `devenv shell`: dev shell
- `devenv up`: run dev processes (supervisor + services)

### Nix
- `nix build .#supervisor`
- `nix build .#identity`
- `nix build .#fs`
- (optional) `nix flake check`

### Manual
```sh
go build -o ./bin/ ./cmd/...
export STRATA_RUNTIME_DIR=/tmp/strata
mkdir -p "$STRATA_RUNTIME_DIR"
./bin/supervisor
```

---

## Smoke Tests

Run with supervisor active in another terminal:

```sh
sh scripts/smoke.sh
```

Every iteration must prove these:

1) issue token
2) list or read file via fs
3) revoke token → access denied (once revocation enforcement is implemented)

If a step doesn't preserve these, revert or fix before continuing.

---

## Implementation Phases

### Phase 1 — Protocol & Client Unification (v0.3.1)
**Objective:** Make IPC usage consistent.

Tasks:
- Update `api/protocol.md` to v0.3.1
- Implement `internal/ipc/client.go`
- Refactor `cmd/strata-ctl` to use the shared IPC client
- Normalize error object across services

Acceptance:
- All calls return consistent `error.code` and `error.name`

---

### Phase 2 — Policy Layer (v0.3.1)
**Objective:** Create the hard boundary.

Tasks:
- Add `internal/policy/authorize.go`
- Add `internal/policy/constraints.go`
  - Implement `path_prefix`
  - Implement `rate_limit` (token bucket, per cap_id)
- Refactor FS handlers to call policy only

Acceptance:
- No-token access always denied (UNAUTHENTICATED)
- Token without right denied (PERMISSION_DENIED)

---

### Phase 3 — Identity Enhancements (v0.3.1)
**Objective:** Make tokens and revoke semantics reliable.

Tasks:
- Implement `identity.introspect`
- Ensure all services check revocation consistently (ideally in auth verifier)
- Ensure revoked caps invalidate existing FS handles

Acceptance:
- Issue token → open handle → revoke cap → read handle fails immediately

---

### Phase 4 — Supervisor State Machine (v0.3.2)
**Objective:** Supervisor becomes runtime kernel.

Tasks:
- Add `internal/service/*` state + lifecycle
- Implement:
  - `supervisor.svc.list`
  - `supervisor.svc.start`
  - `supervisor.svc.stop`
- Crash restart with exponential backoff
- Optional quarantine threshold

Acceptance:
- Killing a service process triggers restart
- svc.list shows accurate transitions

---

### Phase 5 — Registry Service (v0.3.2)
**Objective:** Add local service discovery to enable future cluster.

Tasks:
- Implement `cmd/registry` (memory store)
- Implement `registry.register/resolve/list`
- Supervisor registers identity/fs/registry on startup
- CLI resolves endpoints via registry (fallback to defaults allowed)

Acceptance:
- CLI can resolve services and call them without hardcoded socket paths

---

### Phase 6 — NixOS Module Activation (v0.3.3)
**Objective:** Strata becomes a proper NixOS service.

Tasks:
- Implement NixOS module:
  - `services.strata.enable`
  - RuntimeDirectory (e.g. `/run/strata`)
  - systemd unit for supervisor
  - Options: nodeId, runtimeDir, enable services
- Validate with `nixos-rebuild switch`

Acceptance:
- Enabling the module boots Strata via systemd with correct runtime dir setup

---

## "One Step" Template

For each iteration:

1) Update spec (if needed)
2) Implement code
3) Run:
   - build
   - devenv up
   - smoke test script
4) Commit with message:
   - `step(X): <what changed>`

---

## Review Checklist (Before Merge)

- [ ] Protocol doc updated if methods/fields changed
- [ ] Error codes and names consistent
- [ ] Policy.Authorize() used for protected methods
- [ ] Handles are capability-bound
- [ ] Revocation affects access immediately
- [ ] No new external deps unless justified
- [ ] Build + devenv + smoke pass
