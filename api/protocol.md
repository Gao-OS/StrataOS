# Strata IPC Protocol v0.3.1

## Transport

Unix domain sockets with length-prefixed framing.

**Frame format:** 4-byte big-endian length prefix followed by a JSON payload.

```
[4 bytes: payload length (big-endian uint32)] [N bytes: JSON payload]
```

Maximum frame size: 1 MiB.

## Request Envelope

```json
{
  "v": 1,
  "req_id": "unique-request-id",
  "method": "service.action",
  "auth": {
    "token": "v2.public...."
  },
  "params": {}
}
```

| Field    | Type   | Required | Description                        |
|----------|--------|----------|------------------------------------|
| `v`      | int    | yes      | Protocol version. Must be `1`.     |
| `req_id` | string | yes      | Caller-generated request ID.       |
| `method` | string | yes      | Dotted method name.                |
| `auth`   | object | no       | Authentication context.            |
| `params` | object | no       | Method-specific parameters.        |

## Response Envelope

```json
{
  "v": 1,
  "req_id": "echoed-request-id",
  "ok": true,
  "result": {},
  "error": {
    "code": 3,
    "name": "PERMISSION_DENIED",
    "message": "description",
    "details": {}
  }
}
```

| Field    | Type   | Description                            |
|----------|--------|----------------------------------------|
| `ok`     | bool   | `true` on success, `false` on error.   |
| `result` | any    | Present when `ok` is `true`.           |
| `error`  | object | Present when `ok` is `false`.          |

### Error Object Fields

| Field     | Type   | Description                              |
|-----------|--------|------------------------------------------|
| `code`    | int    | Numeric error code (backward compatible). |
| `name`    | string | Symbolic error name (new in v0.3.1).     |
| `message` | string | Human-readable description.              |
| `details` | object | Optional structured data.                |

## Error Codes

| Code | Name                 | Meaning                                         |
|------|----------------------|-------------------------------------------------|
| 1    | `INVALID_ARGUMENT`   | Invalid request / bad params                    |
| 2    | `UNAUTHENTICATED`    | Authentication required / invalid / expired token |
| 3    | `PERMISSION_DENIED`  | Token valid but lacks rights/constraints        |
| 4    | `NOT_FOUND`          | Resource not found                              |
| 5    | `INTERNAL`           | Internal error                                  |
| 6    | `UNAVAILABLE`        | Service unavailable                             |
| 7    | `RESOURCE_EXHAUSTED` | Rate limit or quota exceeded                    |
| 8    | `CONFLICT`           | State conflict                                  |

Backward compatibility: clients may rely on `code` only.

## Socket Paths

All sockets are created under `STRATA_RUNTIME_DIR` (default: `/run/strata`).

| Service    | Socket                                  |
|------------|-----------------------------------------|
| Supervisor | `$STRATA_RUNTIME_DIR/supervisor.sock`   |
| Identity   | `$STRATA_RUNTIME_DIR/identity.sock`     |
| FS         | `$STRATA_RUNTIME_DIR/fs.sock`           |
| Registry   | `$STRATA_RUNTIME_DIR/registry.sock`     |

## Methods

### identity.issue

Issue a capability token.

**Params:**

| Param         | Type     | Required | Description                                          |
|---------------|----------|----------|------------------------------------------------------|
| `service`     | string   | yes      | Target service (e.g. `"fs"`).                        |
| `actions`     | []string | no       | Backward-compatible action list (`["open","read"]`). |
| `rights`      | []string | no       | Preferred fully-qualified rights (`["fs.open","fs.read"]`). |
| `path_prefix` | string   | no       | Filesystem path constraint.                          |
| `ttl_seconds` | number   | no       | Token TTL in seconds (default: 3600).                |
| `rate_limit`  | string   | no       | Optional rate limit (e.g. `"50rps"`).                |

**Result:**

```json
{
  "token": "v2.public....",
  "cap_id": "hex-id",
  "expires": 1700000000
}
```

### identity.revoke

Revoke a capability by ID.

**Params:**

| Param    | Type   | Required | Description                |
|----------|--------|----------|----------------------------|
| `cap_id` | string | yes      | Capability ID to revoke.   |

### identity.introspect

Decode and validate a token (debugging and tooling).

**Params:**

| Param   | Type   | Required |
|---------|--------|----------|
| `token` | string | yes      |

**Result:**

```json
{
  "claims": { ... }
}
```

### fs.open

Open a file and return a handle. Requires `fs.open` right.

**Params:**

| Param  | Type   | Required | Description                              |
|--------|--------|----------|------------------------------------------|
| `path` | string | yes      | Relative path only. Must not start with `/`. |
| `mode` | string | no       | `"r"` (default). Future: `"rw"`.         |

**Rules:**

- Path must be normalized.
- `..` traversal is rejected.
- Final resolved path must remain under `path_prefix`.

**Result:**

```json
{ "handle": "h1" }
```

### fs.read

Read from an open handle. Requires `fs.read` right.

**Params:**

| Param    | Type   | Required | Description                      |
|----------|--------|----------|----------------------------------|
| `handle` | string | yes      | Handle from `fs.open`.           |
| `offset` | number | no       | Byte offset (default: 0).        |
| `size`   | number | no       | Bytes to read (default: 4096).   |

**Result:**

```json
{
  "data_b64": "....",
  "bytes_read": 42,
  "eof": false
}
```

- `data_b64` is base64-encoded binary.
- For backward compatibility, implementations MAY include `"data"` (plain string) for UTF-8 content.

### fs.list

List directory entries. Requires `fs.list` right.

**Params:**

| Param  | Type   | Required | Description              |
|--------|--------|----------|--------------------------|
| `path` | string | yes      | Relative directory path. |

**Result:**

```json
{
  "entries": [
    { "name": "file.txt", "is_dir": false, "size": 1024 },
    { "name": "subdir",   "is_dir": true,  "size": 4096 }
  ]
}
```

### Handle Semantics

A handle is implicitly bound to:

- `cap_id`
- `service`
- issuing subject

If `identity.revoke(cap_id)` is called:

- All associated handles MUST become invalid immediately.
- Access using invalidated handle MUST return: `UNAUTHENTICATED` or `PERMISSION_DENIED` consistently.

## Supervisor Methods

### supervisor.status

Return supervisor status.

**Result:**

```json
{
  "node_id": "node-1",
  "uptime_sec": 12345
}
```

### supervisor.svc.list

List services and states.

**Result:**

```json
{
  "services": [
    { "name": "identity", "state": "Healthy", "pid": 123 }
  ]
}
```

States:

- `Declared`
- `Starting`
- `Healthy`
- `Crashed`
- `Restarting`
- `Stopped`
- `Quarantined`

### supervisor.svc.start

Start a service.

**Params:**

```json
{ "name": "fs" }
```

### supervisor.svc.stop

Stop a service.

**Params:**

```json
{ "name": "fs", "drain_ms": 2000 }
```

## Registry Methods

### registry.register

Register a service endpoint.

**Params:**

```json
{
  "service": "fs",
  "endpoint": "unix:///run/strata/fs.sock",
  "api_v": 1
}
```

### registry.resolve

Resolve a service endpoint.

**Params:**

```json
{ "service": "fs" }
```

**Result:**

```json
{
  "endpoint": "unix:///run/strata/fs.sock",
  "api_v": 1
}
```

### registry.list

List all registered services.

**Result:**

```json
{
  "services": [
    { "service": "fs", "endpoint": "...", "api_v": 1 }
  ]
}
```

## Token Format

PASETO v2.public tokens signed with ed25519.

Example claims:

```json
{
  "jti": "capability-id",
  "sub": "capability",
  "iss": "identity@node-1",
  "aud": "strata",
  "iat": "2024-01-01T00:00:00Z",
  "exp": "2024-01-01T01:00:00Z",
  "service": "fs",
  "actions": ["open", "read", "list"],
  "rights": ["fs.open", "fs.read", "fs.list"],
  "constraints": {
    "path_prefix": "/allowed/path",
    "rate_limit": "50rps"
  }
}
```

## Authorization Model

- Token must be present for protected methods.
- Token must be valid, not expired, not revoked.
- Rights must match requested method.
- Constraints must be enforced by the target service.
- Deny-by-default policy applies.

## Audit Events (Recommended)

Implementations SHOULD emit structured audit events for:

- `cap.issued`
- `cap.revoked`
- `auth.denied`
- `fs.open`
- `fs.read`
- `svc.start`
- `svc.stop`
- `svc.crash`

## Versioning

- Protocol version is fixed at `v = 1`.
- Backward-compatible extensions MAY add fields.
- Breaking changes require incrementing protocol version.
