# Strata IPC Protocol

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
    "code": 1,
    "message": "description"
  }
}
```

| Field    | Type   | Description                            |
|----------|--------|----------------------------------------|
| `ok`     | bool   | `true` on success, `false` on error.   |
| `result` | any    | Present when `ok` is `true`.           |
| `error`  | object | Present when `ok` is `false`.          |

## Error Codes

| Code | Meaning            |
|------|--------------------|
| 1    | Invalid request    |
| 2    | Authentication required |
| 3    | Permission denied  |
| 4    | Not found          |
| 5    | Internal error     |

## Socket Paths

All sockets are created under `STRATA_RUNTIME_DIR` (default: `/run/strata`).

| Service    | Socket                         |
|------------|--------------------------------|
| Supervisor | `$STRATA_RUNTIME_DIR/supervisor.sock` |
| Identity   | `$STRATA_RUNTIME_DIR/identity.sock`   |
| FS         | `$STRATA_RUNTIME_DIR/fs.sock`         |

## Methods

### identity.issue

Issue a capability token.

**Params:**

| Param         | Type     | Required | Description                              |
|---------------|----------|----------|------------------------------------------|
| `service`     | string   | yes      | Target service (e.g. `"fs"`).            |
| `actions`     | []string | yes      | Permitted actions (e.g. `["open","read","list"]`). |
| `path_prefix` | string   | no       | Filesystem path constraint.              |
| `ttl_seconds` | number   | no       | Token TTL in seconds (default: 3600).    |

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

| Param    | Type   | Required | Description          |
|----------|--------|----------|----------------------|
| `cap_id` | string | yes      | Capability ID to revoke. |

### fs.open

Open a file and return a handle. Requires token with `open` action.

**Params:**

| Param  | Type   | Required | Description       |
|--------|--------|----------|-------------------|
| `path` | string | yes      | File path to open. |

**Result:**

```json
{ "handle": "h1" }
```

### fs.read

Read from an open handle. Requires token with `read` action.

**Params:**

| Param    | Type   | Required | Description                      |
|----------|--------|----------|----------------------------------|
| `handle` | string | yes      | Handle from `fs.open`.           |
| `offset` | number | no       | Byte offset (default: 0).        |
| `size`   | number | no       | Bytes to read (default: 4096).   |

**Result:**

```json
{
  "data": "file contents...",
  "bytes_read": 42
}
```

### fs.list

List directory entries. Requires token with `list` action.

**Params:**

| Param  | Type   | Required | Description            |
|--------|--------|----------|------------------------|
| `path` | string | yes      | Directory path to list. |

**Result:**

```json
{
  "entries": [
    { "name": "file.txt", "is_dir": false, "size": 1024 },
    { "name": "subdir",   "is_dir": true,  "size": 4096 }
  ]
}
```

### supervisor.status

Return supervisor and service status (stub).

**Params:** none.

## Token Format

PASETO v2.public tokens signed with ed25519. Claims:

```json
{
  "jti": "capability-id",
  "sub": "capability",
  "iat": "2024-01-01T00:00:00Z",
  "exp": "2024-01-01T01:00:00Z",
  "service": "fs",
  "actions": ["open", "read", "list"],
  "constraints": {
    "path_prefix": "/allowed/path"
  }
}
```
