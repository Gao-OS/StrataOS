#!/usr/bin/env sh
set -e

# Strata smoke test.
# Assumes supervisor is already running and binaries are built in ./bin/.

CTL="./bin/strata-ctl"

if [ ! -x "$CTL" ]; then
  echo "FAIL: $CTL not found. Run: go build -o ./bin/ ./cmd/..."
  exit 1
fi

echo "=== Strata Smoke Test ==="

# 1. Check supervisor is running.
echo "[1/9] Checking supervisor status..."
$CTL supervisor.status > /dev/null
echo "  OK: supervisor responding"

# 2. List services via supervisor.
echo "[2/9] Listing services via supervisor..."
SVC_LIST=$($CTL supervisor.svc.list)
echo "$SVC_LIST" | grep -q '"services"' || {
  echo "  FAIL: supervisor.svc.list did not return services"
  echo "  Response: $SVC_LIST"
  exit 1
}
echo "  OK: supervisor.svc.list returned service list"

# 3. Check registry is running and has services.
echo "[3/9] Listing services via registry..."
REG_LIST=$($CTL registry.list)
echo "$REG_LIST" | grep -q '"services"' || {
  echo "  FAIL: registry.list did not return services"
  echo "  Response: $REG_LIST"
  exit 1
}
echo "  OK: registry.list returned service list"

# 4. Resolve a service via registry.
echo "[4/9] Resolving identity via registry..."
# Give a moment for async registration to complete.
sleep 1
RESOLVE_RESULT=$($CTL registry.resolve '{"service":"identity"}')
echo "$RESOLVE_RESULT" | grep -q '"endpoint"' || {
  echo "  FAIL: registry.resolve did not return endpoint"
  echo "  Response: $RESOLVE_RESULT"
  exit 1
}
echo "  OK: registry.resolve returned endpoint for identity"

# 5. Issue a capability token.
echo "[5/9] Issuing capability token..."
ISSUE_RESULT=$($CTL identity.issue '{"service":"fs","actions":["open","read","list"],"path_prefix":"/tmp"}')
TOKEN=$(echo "$ISSUE_RESULT" | grep -o '"token": *"[^"]*"' | head -1 | sed 's/.*"token": *"//;s/"//')
CAP_ID=$(echo "$ISSUE_RESULT" | grep -o '"cap_id": *"[^"]*"' | head -1 | sed 's/.*"cap_id": *"//;s/"//')

if [ -z "$TOKEN" ]; then
  echo "  FAIL: no token in response"
  echo "  Response: $ISSUE_RESULT"
  exit 1
fi
echo "  OK: token issued (cap_id=$CAP_ID)"

# 6. Use token to list via relative path (protocol requires relative paths with path_prefix).
echo "[6/9] Listing with capability token (relative path)..."
mkdir -p /tmp/strata-smoke
LIST_RESULT=$($CTL -token "$TOKEN" fs.list '{"path":"strata-smoke"}')
echo "$LIST_RESULT" | grep -q '"ok": *true' || {
  echo "  FAIL: fs.list returned error"
  echo "  Response: $LIST_RESULT"
  exit 1
}
echo "  OK: fs.list succeeded"

# 7. Open and read a file via handle.
echo "[7/9] Open + read via handle binding..."
echo "smoke-test-data" > /tmp/strata-smoke-test.txt
OPEN_RESULT=$($CTL -token "$TOKEN" fs.open '{"path":"strata-smoke-test.txt"}')
HANDLE=$(echo "$OPEN_RESULT" | grep -o '"handle": *"[^"]*"' | head -1 | sed 's/.*"handle": *"//;s/"//')
if [ -z "$HANDLE" ]; then
  echo "  FAIL: no handle returned"
  exit 1
fi
READ_RESULT=$($CTL -token "$TOKEN" fs.read "{\"handle\":\"$HANDLE\"}")
echo "$READ_RESULT" | grep -q "smoke-test-data" || {
  echo "  FAIL: read did not return expected data"
  echo "  Response: $READ_RESULT"
  exit 1
}
echo "  OK: open + read succeeded (handle=$HANDLE)"
rm -f /tmp/strata-smoke-test.txt

# 8. Revoke the capability.
echo "[8/9] Revoking capability..."
$CTL identity.revoke "{\"cap_id\":\"$CAP_ID\"}" > /dev/null
echo "  OK: capability revoked"

# 9. Verify revocation is enforced — fs.list must fail.
echo "[9/9] Verifying revocation enforcement..."
REVOKED_RESULT=$($CTL -token "$TOKEN" fs.list '{"path":"strata-smoke"}' 2>&1) || true
echo "$REVOKED_RESULT" | grep -q '"ok": *false' || {
  echo "  FAIL: fs.list succeeded after revocation (expected denial)"
  exit 1
}
echo "$REVOKED_RESULT" | grep -q "revoked" || {
  echo "  FAIL: error message does not mention revocation"
  echo "  Response: $REVOKED_RESULT"
  exit 1
}
echo "  OK: fs.list denied after revocation"

rm -rf /tmp/strata-smoke

echo ""
echo "=== Smoke Test PASSED ==="
