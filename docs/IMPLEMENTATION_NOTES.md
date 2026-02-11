# Strata Implementation Notes

## Invariants

These hold across all Strata services and must not be violated:

1. **Deny by default.** No access without a valid, unexpired, unrevoked capability token.
2. **Centralized issuance.** Only the identity service issues tokens. No service mints its own.
3. **Centralized policy.** Authorization logic must live in `internal/policy`. Services call `Authorize()`, not ad-hoc checks.
4. **Local verification.** Services verify tokens locally using the identity public key. No round-trip to identity on every request.
5. **Capability scoping.** Tokens are scoped to a specific service, set of actions/rights, and constraints. Broader access requires a new token.
6. **Handle binding.** File handles are bound to the capability that opened them. Revoking the capability invalidates the handle.
7. **Service discovery via registry.** Endpoints discovered via registry (v0.3.2+), not hardcoded.

## Practical Boundaries

- Strata does not implement a filesystem format; it mediates access to Linux FS.
- Strata does not replace systemd; supervisor is logical runtime kernel, systemd is last-resort PID1 supervisor.
- Cluster is staged; do not implement distributed consensus early.
- No ambient authority: services start with minimal privileges and acquire capabilities explicitly.

## Naming Conventions

| Entity          | Convention                | Example                  |
|-----------------|---------------------------|--------------------------|
| Go packages     | lowercase, single word    | `ipc`, `auth`, `policy`  |
| Binaries        | lowercase, hyphenated     | `strata-ctl`             |
| IPC methods     | `service.action`          | `fs.open`, `identity.issue` |
| Error names     | `UPPER_SNAKE_CASE`        | `PERMISSION_DENIED`      |
| Socket files    | `service.sock`            | `identity.sock`          |
| Env variables   | `STRATA_UPPER_SNAKE`      | `STRATA_RUNTIME_DIR`     |
| Capability IDs  | hex-encoded random bytes  | `6afc36c2484a8db0...`    |
| Rights          | fully-qualified           | `fs.open`, `fs.read`     |

- Rights should converge to fully-qualified form: `fs.open`, `fs.read`, etc.
- Legacy `actions` exist for backward compatibility but should be gradually replaced.

## Key Design Decisions

### PASETO v2.public (not JWT)
- No algorithm confusion attacks
- Ed25519 only — no RSA, no HMAC ambiguity
- Implemented with Go stdlib (no external crypto dependencies)

### Length-prefixed JSON (not HTTP, not gRPC)
- Minimal framing overhead for local IPC
- No HTTP parsing complexity
- No protobuf compilation step
- 4-byte big-endian length prefix is simple to implement in any language

### Public key file (not key exchange protocol)
- Identity writes `identity.pub` to the runtime directory
- Other services read it on startup
- Simple, auditable, no handshake protocol needed for local-only deployment

## Future: Cluster Considerations

When multi-node support is added:

- Node identity will be a persistent keypair (not regenerated on startup)
- Capability delegation will require cross-node token chains
- Registry will need distributed consensus or gossip
- IPC transport may extend beyond UDS (e.g., QUIC for inter-node)
- The local invariants above still apply within each node
- Isolate OS dependencies in an adapter layer (future ADR)
