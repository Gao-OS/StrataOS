# Strata Policy Layer — Code Review Report

Review of `internal/policy/authorize.go`, `internal/policy/constraints.go`,
`internal/capability/capability.go`, `cmd/fs/main.go`, `cmd/identity/main.go`,
and `internal/ipc/types.go`.

## Checklist Summary

| # | Item | Verdict |
|---|------|---------|
| 1 | Scope & purpose | **PASS** |
| 2 | Authorization correctness | **PASS** |
| 3 | Constraint enforcement | **FAIL** (3 issues) |
| 4 | FS handle binding/validation | **PASS w/ issue** |
| 5 | Security best practices | **PASS w/ issue** |
| 6 | Concurrency & safety | **PASS** |
| 7 | Maintainability | **PASS** |
| 8 | Test coverage | **FAIL** |
| 9 | Audit logging hooks | **FAIL** |
| 10 | Diff-specific checks | **PASS w/ issue** |
| 11 | Error handling consistency | **PASS w/ issue** |

---

## 1. Scope & Purpose — PASS

| Sub-check | Verdict | Evidence |
|-----------|---------|----------|
| Authorization decisions centralized | PASS | All FS handlers call `policy.Authorize()` before I/O |
| No service performs auth outside policy | PASS | Grep `HasAction\|HasRight\|\.Actions\|\.Rights` in `cmd/` returns only `cap.Rights = rights` in identity (assignment, not check) |
| Constraints enforced correctly | PASS w/ issues | See checklist item 3 |

All three FS handlers (`fs.open` at `cmd/fs/main.go:161`, `fs.read` at `:187`, `fs.list` at `:237`) route through `policy.Authorize()`.
The only non-policy permission logic is the revocation check in FS handlers, which is correct — revocation is handle-table state, not authorization policy.

---

## 2. Authorization Correctness — PASS

| Sub-check | Verdict | File:Line |
|-----------|---------|-----------|
| All FS methods go through `policy.Authorize()` | PASS | `cmd/fs/main.go:161,187,237` |
| No direct string comparisons of method names outside policy | PASS | Grep confirms zero matches in `cmd/` |
| Required rights derived consistently (`fs.open` → `"fs.open"`) | PASS | `authorize.go:43-51` — `SplitN(method, ".", 2)` derives service and action from the method string itself |
| Legacy actions fallback mapped to rights | PASS | `authorize.go:63` — `hasRight(rights, method) \|\| hasAction(actions, action)` |
| `Authorize()` returns UNAUTHENTICATED for nil claims | PASS | `authorize.go:34-40` — code 2, name `"UNAUTHENTICATED"` |
| `Authorize()` returns PERMISSION_DENIED for service mismatch | PASS | `authorize.go:54-60` — code 3, name `"PERMISSION_DENIED"` |
| `Authorize()` returns PERMISSION_DENIED for missing right/action | PASS | `authorize.go:63-69` — code 3 |
| Rights logic not duplicated in FS handlers | PASS | FS handlers only call `policy.Authorize()` and `handles.IsRevoked()` |
| Method-to-right mapping centralized | PASS | `authorize.go:63` — rights match the full method string, actions match the action portion, both computed from the single `method` parameter |

**Deny-by-default**: Every code path in `Authorize()` that doesn't reach the final `return enforceConstraints(...)` returns a `PolicyError`. The constraints themselves also default to deny on violation. Correct.

---

## 3. Constraint Enforcement — FAIL (3 issues)

### 3.1 Path Prefix

| Sub-check | Verdict | File:Line |
|-----------|---------|-----------|
| Rejects `..` traversal | PASS | `constraints.go:36-42` — `strings.Contains(path, "..")` |
| Rejects leading `/` (enforces relative paths) | **FAIL** | Not checked anywhere. See **Issue 3c** |
| Normalization before prefix check | PASS | `constraints.go:44-59` — `filepath.Abs()` on both path and prefix |
| Prefix comes from token claims, not hardcoded | PASS | `constraints.go:16` — `claims.Constraints.PathPrefix` |
| Separator boundary check (prevents `/tmp` matching `/tmpevil`) | PASS | `constraints.go:61` — `absPrefix + string(filepath.Separator)` |

### 3.2 Rate Limit

| Sub-check | Verdict | File:Line |
|-----------|---------|-----------|
| Keyed by `cap_id` (not shared global) | PASS | `constraints.go:112` — `globalLimiter.buckets[capID]` |
| Concurrency-safe | PASS | `constraints.go:109-110` — `sync.Mutex` around all bucket operations |
| Capacity/refill logic sane | PASS | `constraints.go:118-124` — token bucket with time-based refill, capped at rate |
| Returns `RESOURCE_EXHAUSTED` when exceeded | PASS | `constraints.go:127-131` — code 7, name `"RESOURCE_EXHAUSTED"` |
| Buckets cleaned up | **FAIL** | Never removed. See **Issue 3a** |

### Issue 3a: Rate limiter buckets never cleaned up — MEDIUM
- **File**: `internal/policy/constraints.go:72-74`
- **Problem**: `globalLimiter.buckets` grows unboundedly. Each unique `cap_id` creates a permanent entry. Long-running FS processes leak memory proportional to the total number of distinct capabilities ever used.
- **Fix**: Add lazy eviction — when inserting a new bucket, scan and remove entries where `time.Since(b.last) > 2 * ttl`. Alternatively, expose `RemoveBucket(capID)` and call it from the `fs.revoke` handler.

### Issue 3b: Malformed rate limit string fails open — LOW
- **File**: `internal/policy/constraints.go:104-107`
- **Problem**: If `parseRate()` fails (e.g. `"50rpm"`, `"abc"`, `"0rps"`), `enforceRateLimit` returns `nil`, silently allowing unlimited requests. The token issuer set a rate limit, but it's not enforced.
- **Fix**: Return `INVALID_ARGUMENT` (code 1) when a non-empty rate limit string is unparseable. At minimum, log a warning.

### Issue 3c: Leading `/` not rejected — paths must be relative per protocol — MEDIUM
- **File**: `api/protocol.md:158` specifies `fs.open` path is "Relative path only. Must not start with `/`." Same for `fs.list` at `:205`.
- **Problem**: Neither `enforcePathPrefix` in `constraints.go` nor the FS handlers in `cmd/fs/main.go` reject absolute paths. The smoke test (`scripts/smoke.sh:36`) itself sends `{"path":"/tmp"}` — an absolute path. Security is maintained because `filepath.Abs()` + prefix comparison works with absolutes, but the implementation violates its own protocol spec.
- **Fix**: Add rejection of leading `/` in `enforcePathPrefix` (before normalization):
  ```go
  if strings.HasPrefix(path, "/") {
      return &PolicyError{Code: CodePermissionDenied, Name: "PERMISSION_DENIED",
          Message: "absolute paths not allowed; use relative paths under path_prefix"}
  }
  ```
  Then update `smoke.sh` and the protocol if absolute paths are actually desired.

---

## 4. FS Handle Binding & Validation — PASS w/ 1 issue

| Sub-check | Verdict | File:Line |
|-----------|---------|-----------|
| Handle stores `cap_id` | PASS | `cmd/fs/main.go:29` — `handleEntry.capID` |
| Token revalidated for each handle use | PASS | `fs.read` at `:181-188` calls `extractClaims` + `policy.Authorize` before touching the handle |
| Handle without matching `cap_id` → PERMISSION_DENIED | PASS | `cmd/fs/main.go:201-203` — `entry.capID != claims.ID` returns code 3 |
| Revoked cap invalidates handle immediately | PASS | `cmd/fs/main.go:205-207` — `handles.IsRevoked(entry.capID)` checked on every read; `fs.revoke` handler at `:268-276` marks cap_id as revoked |

### Issue 4a: `fs.revoke` handler has no caller authentication — MEDIUM
- **File**: `cmd/fs/main.go:268-276`
- **Problem**: The `fs.revoke` IPC handler is an internal endpoint that any process with access to `fs.sock` can invoke — no token required, no caller verification. A rogue process could revoke arbitrary capabilities by sending `{"method":"fs.revoke","params":{"cap_id":"..."}}` to the socket.
- **Mitigation**: Unix socket file permissions restrict access to authorized users/groups. Acceptable for MVP.
- **Fix for hardening**: Verify caller via `SO_PEERCRED` (check UID/PID), or require a shared internal bearer token for service-to-service calls.

---

## 5. Security Best Practices — PASS w/ 1 issue

| Sub-check | Verdict | File:Line |
|-----------|---------|-----------|
| Auth checks before business logic | PASS | All 3 FS handlers: `extractClaims` → `policy.Authorize` → revocation → I/O |
| Centralized policy prevents inconsistent enforcement | PASS | Single `Authorize()` function used by all methods |
| Input validation for malformed inputs | PASS | Empty paths, empty handles, empty cap_ids all caught |
| No uncovered code paths bypass policy | PASS | `fs.revoke` is internal-only (no user-facing auth needed by design) |
| No secrets/keys logged or exposed in errors | PASS | Errors contain only codes/messages; `auth.Verify` errors don't leak key material; PASETO signing errors are opaque |
| Error messages don't leak internal state | PASS | Policy errors return method names and paths (acceptable), no stack traces or internal details |

### Issue 5a: `capability.HasAction()` is dead code — LOW
- **File**: `internal/capability/capability.go:49-56`
- **Problem**: `HasAction()` is defined on `Capability` but never called anywhere in the codebase. The policy package implements its own `hasAction()` at `authorize.go:84-91`. Dead code increases maintenance surface and could mislead future developers into using the wrong function.
- **Fix**: Delete `HasAction()` from `capability.go`.

---

## 6. Concurrency & Safety — PASS

| Sub-check | Verdict | File:Line |
|-----------|---------|-----------|
| Rate limit struct uses thread-safe data structures | PASS | `constraints.go:77` — `sync.Mutex` |
| Mutex/sync for shared state | PASS | `handleTable` uses `sync.RWMutex` (`cmd/fs/main.go:36`); `rateLimiter` uses `sync.Mutex`; `RevocationList` uses `sync.RWMutex` |
| No data races via unprotected maps | PASS | All map accesses are under locks. `handleTable.nextID` uses `atomic.Uint64` (`cmd/fs/main.go:39`) |
| No unsynchronized global variables | PASS | Only global is `globalLimiter` (`constraints.go:72`), protected by its own mutex |

Lock discipline is correct: `RLock` for read-only paths (`Get`, `IsRevoked`), `Lock` for mutations (`Open`, `Revoke`, `CloseAll`).

---

## 7. Maintainability — PASS

| Sub-check | Verdict | Evidence |
|-----------|---------|----------|
| Naming consistent and descriptive | PASS | `hasRight`/`hasAction` (policy), `Authorize`/`PolicyError` (public API), `enforcePathPrefix`/`enforceRateLimit` (constraint functions) |
| Legacy actions → new rights mapping explicit | PASS | `authorize.go:62-63` comment: "Check fully-qualified rights (preferred) or legacy actions (fallback)" |
| Policy functions documented | PASS | `Authorize()`, `PolicyError`, and both constraint functions have doc comments |
| No duplicated logic in services beyond calling policy | PASS | FS handlers only call `policy.Authorize()` + handle-table methods |

---

## 8. Test Coverage — FAIL

**No test files exist.** `**/*_test.go` returns zero results across the entire repository.

### Required tests (priority order):

1. **`internal/policy/` tests** (highest priority):
   - `Authorize()`: nil claims, wrong service, missing right, missing action, rights-preferred-over-actions, both rights+actions present
   - `enforcePathPrefix()`: `..` rejection, prefix boundary (`/tmp` vs `/tmpevil`), empty prefix (pass-through), empty path (pass-through), absolute paths
   - `enforceRateLimit()`: basic exhaustion, token refill over time, malformed rate string, empty rate string, per-cap isolation

2. **`internal/auth/` tests**:
   - PASETO sign/verify round-trip
   - Verify with wrong key → error
   - Tampered token → error
   - Expired token detection

3. **Handle table tests** (integration-level):
   - Open returns valid handle
   - Get with invalid handle → not found
   - Cross-capability binding check
   - Revoke → IsRevoked returns true
   - Concurrent open/read/revoke (race detector)

---

## 9. Audit Logging Hooks — FAIL

Protocol v0.3.1 (`api/protocol.md:372-384`) recommends structured audit events for:
`cap.issued`, `cap.revoked`, `auth.denied`, `fs.open`, `fs.read`, `svc.start`, `svc.stop`, `svc.crash`.

### Current state:
- Implementation uses `log.Printf` with informal `[fs]`/`[identity]` prefixes
- **`auth.denied` events are never emitted** — `policy.Authorize()` returns errors silently, and callers in FS don't log denials
- No structured JSON audit events anywhere
- No audit hook or interface exists (not even a stub)

### Required audit event fields per checklist:
- `req_id` — available from `ipc.Request.ReqID`
- `subject` — available from `claims.Subject`
- `cap_id` — available from `claims.ID`
- `action` — the `method` parameter
- `resource` — `ctx["path"]` for FS operations

### Recommendation:
Create `internal/audit/audit.go` with:
```go
type Event struct {
    Type     string         `json:"type"`      // "auth.denied", "fs.open", etc.
    ReqID    string         `json:"req_id"`
    Subject  string         `json:"subject"`
    CapID    string         `json:"cap_id"`
    Action   string         `json:"action"`
    Resource string         `json:"resource,omitempty"`
    Detail   string         `json:"detail,omitempty"`
    Time     time.Time      `json:"time"`
}
func Emit(e Event) { ... }
```
Hook into:
- `policy.Authorize()` failure → `auth.denied`
- `cmd/identity/main.go:88` (issue success) → `cap.issued`
- `cmd/identity/main.go:104` (revoke success) → `cap.revoked`
- `cmd/fs/main.go:176` (open success) → `fs.open`
- `cmd/fs/main.go:220` (read success) → `fs.read`

---

## 10. Diff-Specific Checks — PASS w/ 1 issue

### `internal/capability/capability.go`

| Sub-check | Verdict | Line |
|-----------|---------|------|
| `Rights []string` field added | PASS | `:19` — `Rights []string \`json:"rights,omitempty"\`` |
| `Constraints.RateLimit` field added | PASS | `:26` — `RateLimit string \`json:"rate_limit,omitempty"\`` |
| Fields documented | PASS | Struct comments on `:12` and `:24` |
| JSON tags correct | PASS | `omitempty` on both optional fields |

### `cmd/identity/main.go`

| Sub-check | Verdict | Line |
|-----------|---------|------|
| Accepts both legacy `actions` and new `rights` | PASS | `:48-55` (actions), `:56-63` (rights) |
| Validation correct (requires at least one of actions/rights) | PASS | `:65-67` — `len(actions) == 0 && len(rights) == 0` |
| `rate_limit` param accepted | PASS | `:70` |
| `path_prefix` param accepted | PASS | `:69` |
| Rights assigned to capability | PASS | `:81` — `cap.Rights = rights` |

### `cmd/fs/main.go`

| Sub-check | Verdict | Line |
|-----------|---------|------|
| Old inline permission logic removed | PASS | No `HasAction`/`HasRight` calls remain in `cmd/` |
| All methods route through `policy.Authorize()` | PASS | `:161` (open), `:187` (read), `:237` (list) |
| Constraint context passed correctly (path) | PASS | `:161` passes `map[string]any{"path": path}` for open; `:237` same for list; `:187` passes `nil` for read (handle-only, no path context needed) |

### Issue 10a: `ipc.Error` struct missing `name` and `details` fields — MEDIUM
- **File**: `internal/ipc/types.go:26-29`
- **Problem**: Protocol v0.3.1 (`api/protocol.md:46-51,60-67`) defines error objects with 4 fields: `code`, `name`, `message`, `details`. The `ipc.Error` struct only has `Code` and `Message`. The `PolicyError` struct in `policy/authorize.go:20-24` correctly has `Name`, but `policyError()` at `cmd/fs/main.go:116-118` drops it when calling `ipc.ErrorResponse(reqID, pe.Code, pe.Message)` because `ErrorResponse` doesn't accept a `name` parameter.
- **Impact**: All error responses on the wire lack the `name` and `details` fields, violating protocol v0.3.1.
- **Fix**:
  ```go
  // ipc/types.go
  type Error struct {
      Code    int            `json:"code"`
      Name    string         `json:"name,omitempty"`
      Message string         `json:"message"`
      Details map[string]any `json:"details,omitempty"`
  }
  ```
  Update `ErrorResponse` to accept `name`, or add a `FullErrorResponse()` helper. Update `policyError()` to pass through `pe.Name`.

---

## 11. Error Handling & Consistency — PASS w/ 1 issue

| Sub-check | Verdict | Evidence |
|-----------|---------|----------|
| Errors use unified codes and names | PASS | `PolicyError` always uses `Code` + `Name` from the `policy` constants |
| No `fmt.Errorf` for permission errors without structured envelope | PASS | Grep for `fmt.Errorf.*perm\|denied\|unauth` returns zero matches in all `.go` files |
| Checks occur before sensitive operations | PASS | All 3 FS handlers: `extractClaims` → `policy.Authorize` → revocation check → file I/O |
| Error codes aligned between policy and IPC | PASS | `policy.CodePermissionDenied = 3` matches `ipc.ErrPermDenied = 3`; `policy.CodeUnauthenticated = 2` matches `ipc.ErrAuthRequired = 2` |

### Issue 11a: Identity→FS revocation notify is fire-and-forget — LOW
- **File**: `cmd/identity/main.go:108-115`
- **Problem**: If FS is temporarily down when `identity.revoke` fires, the `ipc.SendRequest` to `fs.sock` fails silently (logged at `:114` but not retried). The revoked capability remains valid at FS until FS restarts — and even then, the in-memory revoked set is empty on startup.
- **Impact**: Time window where a revoked capability can still access FS if FS was unreachable during revocation.
- **Fix**: Acceptable for MVP. For production: persist revocations (e.g., write to `$RUNTIME_DIR/revoked.json`), have FS load them on startup, or implement a periodic sync/poll from FS to identity.

---

## Issues Summary

| ID | Severity | File:Line | Issue | Fix |
|----|----------|-----------|-------|-----|
| 3a | **Medium** | `policy/constraints.go:72-74` | Rate limiter buckets never cleaned up (memory leak) | Add lazy eviction or hook cleanup into revocation |
| 3b | Low | `policy/constraints.go:104-107` | Malformed rate limit fails open silently | Return `INVALID_ARGUMENT` or log warning |
| 3c | **Medium** | `policy/constraints.go:27-68` | Absolute paths accepted; protocol requires relative-only | Reject paths starting with `/` in `enforcePathPrefix` |
| 4a | **Medium** | `cmd/fs/main.go:268-276` | `fs.revoke` handler has no caller authentication | Add `SO_PEERCRED` check or shared internal secret |
| 5a | Low | `capability/capability.go:49-56` | `HasAction()` is dead code | Delete it |
| 8 | **High** | N/A | Zero test files in entire repository | Add unit tests: policy → constraints → auth → handles |
| 9 | **Medium** | N/A | No structured audit events; `auth.denied` never emitted | Create `internal/audit/` with structured JSON events |
| 10a | **Medium** | `ipc/types.go:26-29` | `ipc.Error` missing `name`/`details` per protocol v0.3.1 | Add fields to struct, update `ErrorResponse` helper |
| 11a | Low | `cmd/identity/main.go:108-115` | Revocation notify is fire-and-forget | Persist revocations or add retry/sync mechanism |

**Total: 9 issues** — 1 high, 5 medium, 3 low.

**No critical security vulnerabilities found.** The core authorization logic is correct and deny-by-default. The main gaps are: test coverage (high), protocol conformance for error structures and relative-path enforcement (medium), and operational concerns (bucket cleanup, audit logging, revocation reliability).
