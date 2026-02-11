# Strata Plan

Strata is a NixOS-native, capability-oriented distributed runtime substrate.
It runs on Linux (NixOS) and provides a logical microkernel/exokernel-style control layer on top of namespaces, cgroups, and systemd.

This project is NOT:
- an AI orchestration system
- a Linux replacement
- a microkernel implementation (at least for v0.3–v0.4)

This project IS:
- a capability-first service runtime substrate
- a modular boundary enforcement layer
- a BEAM-friendly control-plane target (future)
- reproducible build/deploy via Nix flakes
- developed via devenv

---

## Goals

### Primary goals
- Capability-first access mediation (deny-by-default)
- Service graph modularity (clear boundaries)
- Node-local supervisor becomes the "runtime kernel"
- Reproducible builds and deploys (Nix)
- Incremental path to distributed operation

### Non-goals (v0.3–v0.4)
- Writing drivers, filesystems, or a new kernel
- GPU/Android UI integration
- Hard multi-tenant security (seccomp/userns everywhere)
- Kubernetes/Docker as a core dependency

---

## Architecture Summary

Layers:

- **NixOS + Linux**: mechanisms (drivers, namespaces, cgroups)
- **Strata Supervisor**: lifecycle + budgets + reconciliation
- **Core Services**: capability-guarded endpoints (identity, fs, net, registry, audit)
- **Runtimes**: BEAM/POSIX/Android-subsystem (incremental)

Key invariants:
- No ambient authority: services start with minimal privileges
- Access is only via capabilities
- Policy is centralized (Authorize + constraints)
- Every step is incremental and testable

---

## Deliverables

### v0.3 (Baseline)
- UDS IPC + framing + envelope
- identity.issue / identity.revoke
- fs.open / fs.read / fs.list
- PASETO v2.public tokens
- Nix flake + devenv runnable MVP

### v0.3.1 (Hard Boundaries)
- Unified error model (code + name + details)
- Unified IPC client library (internal/ipc/client)
- Central policy layer: Authorize() + constraints
- identity.introspect
- FS handle semantics:
  - handles bound to cap_id
  - revocation invalidates handles
  - path normalization + traversal protection
- Protocol spec updated (v0.3.1)

### v0.3.2 (Runtime Kernel)
- Supervisor state machine:
  - start/stop/list/status
  - crash restart with backoff
  - quarantine threshold (optional)
- Registry service (local):
  - register/resolve/list
- supervisor auto-registers services
- CLI resolves endpoints via registry

### v0.3.3 (NixOS Activation)
- NixOS module actually starts Strata via systemd
- RuntimeDirectory/tmpfiles
- Basic options: enable, nodeId, runtime dir, enable services
- `nixos-rebuild switch` brings Strata up

### v0.4 (Distributed Hooks)
- Prepare issuer/audience and key distribution hooks
- Registry endpoint scheme supports future TCP
- No full cluster implementation yet; only design hooks + minimal stubs

---

## Milestone Checklist

### M1: Protocol & Tooling (v0.3.1)
- [x] `api/protocol.md` updated to v0.3.1
- [ ] `internal/ipc/client.go` created and used by strata-ctl
- [ ] Consistent error object across services

### M2: Policy (v0.3.1)
- [ ] `internal/policy/authorize.go` exists
- [ ] `internal/policy/constraints.go` implements `path_prefix` + `rate_limit`
- [ ] FS handlers call policy only; no ad-hoc permission checks

### M3: Identity & Revocation (v0.3.1)
- [ ] identity.introspect implemented
- [ ] revoke affects validation everywhere
- [ ] revoked cap invalidates existing handles

### M4: Supervisor State Machine (v0.3.2)
- [ ] service states tracked
- [ ] restart/backoff policy works
- [ ] supervisor control methods exist

### M5: Registry (v0.3.2)
- [ ] registry service exists
- [ ] supervisor registers services
- [ ] CLI resolves endpoints via registry

### M6: NixOS Module (v0.3.3)
- [ ] module enables systemd unit
- [ ] runtime dir created automatically
- [ ] nixos-rebuild switch brings system up

---

## Risks & Mitigations

- **Overengineering early** → Keep steps small, ship runnable increments.
- **Policy logic leaking into services** → Enforce Authorize() usage and code review rule.
- **Protocol churn** → Protocol version stays v=1; only backward-compatible additions.
- **Future kernel migration** → isolate OS dependencies in an adapter layer (future ADR).

---

## Governance / Decisions

- Changes to protocol require updating `api/protocol.md` and a compatibility note.
- New services must:
  - define methods in protocol spec
  - use policy.Authorize()
  - register in registry
  - emit audit events (recommended)
