# Strata Policy Layer — Code Review Report

## Checklist Summary

| # | Item | Verdict |
|---|------|---------|
| 1 | Scope & purpose | **PASS** |
| 2 | Authorization correctness | **PASS** |
| 3 | Constraint enforcement | **PASS w/ issues** |
| 4 | FS handle binding/validation | **PASS w/ issues** |
| 5 | Security best practices | **PASS w/ issues** |
| 6 | Concurrency & safety | **PASS** |
| 7 | Maintainability | **PASS** |
| 8 | Test coverage | **FAIL** |
| 9 | Audit logging hooks | **FAIL** |
| 10 | Diff-specific checks | **PASS w/ issues** |
| 11 | Error handling consistency | **PASS w/ issues** |
| 12 | Output format | N/A (review itself) |

---

## 1. Scope & Purpose — PASS

`internal/policy/` is the single authorization chokepoint. All three FS handlers (`fs.open`, `fs.list`, `fs.read`) call `policy.Authorize()` before any I/O. No residual `HasAction`/`HasRight` calls exist in `cmd/`. The only non-policy permission logic is the revocation check in FS, which is correct (revocation is handle-table state, not policy).

---

## 2. Authorization Correctness — PASS

- **Nil claims → UNAUTHENTICATED (code 2)**: Correct (`authorize.go:34-40`).
- **Service scope check**: Token's `Service` field must match the method prefix (`authorize.go:54-60`).
- **Rights-first, actions-fallback**: `hasRight(rights, method) || hasAction(actions, action)` (`authorize.go:63`). Correct per protocol v0.3.1.
- **Deny-by-default**: All paths that don't explicitly authorize return a `PolicyError`.

---

## 3. Constraint Enforcement — PASS w/ 3 issues

### Issue 3a: Rate limiter buckets never cleaned up
- **File**: `internal/policy/constraints.go:72-74`
- **Severity**: Medium
- **Description**: `globalLimiter.buckets` grows unboundedly. Each unique `cap_id` adds an entry that is never removed, even after the capability expires or is revoked. Long-running FS processes will leak memory.
- **Fix**: Add a `Cleanup()` method or lazy eviction (remove buckets where `time.Since(b.last) > threshold`). Could also hook into revocation to delete the bucket for a revoked cap_id.

### Issue 3b: Malformed rate limit fails open
- **File**: `internal/policy/constraints.go:104-107`
- **Severity**: Low
- **Description**: If `parseRate()` returns `false` (e.g. for `"50rpm"` or `"abc"`), `enforceRateLimit` returns `nil` — silently allowing unlimited requests. This is arguably correct (token issuer's mistake), but could be surprising.
- **Fix**: Consider logging a warning when a non-empty rate limit string is unparseable, or returning `INVALID_ARGUMENT`.

### Issue 3c: Protocol says fs.open path "must not start with `/`"
- **File**: `api/protocol.md:158`, `internal/policy/constraints.go:27-68`
- **Severity**: Low (smoke test uses absolute paths and works)
- **Description**: The protocol spec says `fs.open` path is "relative path only, must not start with `/`". The implementation accepts absolute paths fine. The `enforcePathPrefix` function resolves both path and prefix to absolute before comparing, so security is maintained — but protocol compliance is violated.
- **Fix**: Either update the protocol to allow absolute paths or add a check in `fs.open`/`fs.list` handlers rejecting paths starting with `/`.

---

## 4. FS Handle Binding/Validation — PASS w/ 1 issue

- **Handle stores cap_id**: Correct (`cmd/fs/main.go:29`).
- **Cross-capability check**: `entry.capID != claims.ID` denies handle theft (`cmd/fs/main.go:201-203`).
- **Revocation invalidates handles**: `handles.IsRevoked(entry.capID)` checked before read (`cmd/fs/main.go:205-207`).
- **fs.revoke handler**: Marks cap_id as revoked in the local table (`cmd/fs/main.go:268-276`).

### Issue 4a: `fs.revoke` handler is unauthenticated
- **File**: `cmd/fs/main.go:268-276`
- **Severity**: Medium
- **Description**: The `fs.revoke` internal handler accepts any request on the Unix socket without verifying the caller is the identity service. Any process with access to `fs.sock` can revoke arbitrary capabilities. Currently mitigated by Unix socket file permissions, but this is defense-in-depth weakness.
- **Fix**: Add a shared internal secret or verify the caller is the identity service (e.g. via `SO_PEERCRED`). Alternatively, document this as an accepted risk given socket-level isolation.

---

## 5. Security Best Practices — PASS w/ 1 issue

- **Path traversal rejection**: `strings.Contains(path, "..")` before normalization (`constraints.go:36-42`). Correct.
- **Absolute path comparison**: Uses `filepath.Abs()` on both path and prefix with separator boundary check (`constraints.go:61`). Correct.
- **No command injection vectors**: All paths go through `os.Open`/`os.ReadDir`, not shell execution.
- **PASETO token verification**: Crypto verification happens before any authorization check.

### Issue 5a: `capability.HasAction()` is dead code
- **File**: `internal/capability/capability.go:49-56`
- **Severity**: Low (hygiene)
- **Description**: `HasAction()` is defined on `Capability` but never called — the policy package uses its own `hasAction()`. Dead code increases maintenance surface.
- **Fix**: Remove `HasAction()` from `capability.go`.

---

## 6. Concurrency & Safety — PASS

- **handleTable**: Uses `sync.RWMutex` with proper read/write lock discipline. `RLock` for reads, `Lock` for writes. `atomic.Uint64` for handle ID generation.
- **rateLimiter**: Uses `sync.Mutex` (conservative but correct for bucket manipulation).
- **RevocationList** (auth package): Uses `sync.RWMutex`.
- No data races in the IPC server — each request handled in a goroutine, shared state properly synchronized.

---

## 7. Maintainability — PASS

- Clear separation: `policy/` owns authorization, `auth/` owns crypto, `capability/` owns claims, `ipc/` owns framing.
- `PolicyError` struct is well-designed with Code/Name/Message.
- `policyError()` helper in `cmd/fs/main.go:116-121` cleanly converts policy errors to IPC responses.

---

## 8. Test Coverage — FAIL

- **No test files exist**: `**/*_test.go` returns zero results.
- **Missing critical tests**:
  - `policy.Authorize()`: nil claims, wrong service, missing rights, missing actions, rights+actions fallback
  - `enforcePathPrefix()`: traversal, boundary cases, prefix-as-filename attack (e.g. prefix `/tmp` should not allow `/tmpevil`)
  - `enforceRateLimit()`: basic limiting, refill, malformed strings
  - `handleTable`: concurrent open/read/revoke, cross-cap denial
  - PASETO sign/verify round-trip
- **Recommendation**: Priority order — policy → constraints → auth → handle table.

---

## 9. Audit Logging Hooks — FAIL

- Protocol v0.3.1 (`api/protocol.md:372-384`) recommends structured audit events for `cap.issued`, `cap.revoked`, `auth.denied`, `fs.open`, `fs.read`.
- Current implementation uses `log.Printf` with informal `[fs]`/`[identity]` prefixes. No structured audit events.
- `auth.denied` events are not logged at all — `policy.Authorize()` returns errors silently.
- **Recommendation**: Add an `audit.Emit()` function that outputs structured JSON events. Hook it into:
  - `policy.Authorize()` failure path → `auth.denied`
  - `identity.issue` success → `cap.issued`
  - `identity.revoke` success → `cap.revoked`
  - `fs.open` success → `fs.open`

---

## 10. Diff-Specific Checks — PASS w/ 1 issue

### Issue 10a: `ipc.Error` struct missing `name` and `details` fields
- **File**: `internal/ipc/types.go:26-29`
- **Severity**: Medium
- **Description**: Protocol v0.3.1 defines error objects with `code`, `name`, `message`, and `details` fields. The `ipc.Error` struct only has `Code` and `Message`. The `PolicyError` struct correctly includes `Name`, but `policyError()` in `cmd/fs/main.go:116-121` drops the `Name` field when converting to `ipc.ErrorResponse()` because `ErrorResponse()` only accepts `code` and `msg`.
- **Fix**: Add `Name string` and `Details map[string]any` to `ipc.Error`. Update `ErrorResponse()` signature or add `ErrorResponseFull()`. Update `policyError()` to pass through the `Name` field.

---

## 11. Error Handling Consistency — PASS w/ 1 issue

- All FS handlers follow the same pattern: `extractClaims → policy.Authorize → revocation check → operation`.
- Error codes are consistent: `ErrInvalidRequest=1`, `ErrAuthRequired=2`, `ErrPermDenied=3`, etc.
- `PolicyError` codes align with `ipc` error codes.

### Issue 11a: Identity→FS revocation notify is fire-and-forget
- **File**: `cmd/identity/main.go:108-115`
- **Severity**: Low
- **Description**: If the FS service is temporarily down when `identity.revoke` is called, the `fs.revoke` notification silently fails (`log.Printf` only). The revoked capability remains usable at FS until FS restarts (and even then, the revoked set is in-memory only).
- **Fix**: Acceptable for MVP. For production: persist revocations to disk, or have FS periodically sync with identity, or use a persistent notification queue.

---

## Issues Summary

| # | Severity | File:Line | Issue |
|---|----------|-----------|-------|
| 3a | Medium | `policy/constraints.go:72` | Rate limiter buckets never cleaned up (memory leak) |
| 3b | Low | `policy/constraints.go:104-107` | Malformed rate limit fails open silently |
| 3c | Low | `protocol.md:158` vs `constraints.go` | Absolute paths accepted despite protocol saying "relative only" |
| 4a | Medium | `cmd/fs/main.go:268-276` | `fs.revoke` handler has no caller authentication |
| 5a | Low | `capability/capability.go:49-56` | `HasAction()` is dead code |
| 8 | High | N/A | Zero test files in entire repository |
| 9 | Medium | N/A | No structured audit events per protocol recommendation |
| 10a | Medium | `ipc/types.go:26-29` | `ipc.Error` missing `name`/`details` fields from protocol v0.3.1 |
| 11a | Low | `cmd/identity/main.go:108-115` | Revocation notification is fire-and-forget |

**Total: 9 issues** — 1 high, 4 medium, 4 low. No critical security vulnerabilities found. The core authorization logic is correct and deny-by-default. The main gaps are test coverage, protocol conformance for error structures, and operational concerns (memory leaks, audit logging).
