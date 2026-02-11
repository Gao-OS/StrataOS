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
echo "[1/6] Checking supervisor status..."
$CTL supervisor.status > /dev/null
echo "  OK: supervisor responding"

# 2. Issue a capability token.
echo "[2/6] Issuing capability token..."
ISSUE_RESULT=$($CTL identity.issue '{"service":"fs","actions":["open","read","list"],"path_prefix":"/tmp"}')
TOKEN=$(echo "$ISSUE_RESULT" | grep -o '"token": *"[^"]*"' | head -1 | sed 's/.*"token": *"//;s/"//')
CAP_ID=$(echo "$ISSUE_RESULT" | grep -o '"cap_id": *"[^"]*"' | head -1 | sed 's/.*"cap_id": *"//;s/"//')

if [ -z "$TOKEN" ]; then
  echo "  FAIL: no token in response"
  echo "  Response: $ISSUE_RESULT"
  exit 1
fi
echo "  OK: token issued (cap_id=$CAP_ID)"

# 3. Use token to list /tmp.
echo "[3/6] Listing /tmp with capability token..."
LIST_RESULT=$($CTL -token "$TOKEN" fs.list '{"path":"/tmp"}')
echo "$LIST_RESULT" | grep -q '"ok": *true' || {
  echo "  FAIL: fs.list returned error"
  echo "  Response: $LIST_RESULT"
  exit 1
}
echo "  OK: fs.list succeeded"

# 4. Open and read a file via handle.
echo "[4/6] Open + read via handle binding..."
echo "smoke-test-data" > /tmp/strata-smoke-test.txt
OPEN_RESULT=$($CTL -token "$TOKEN" fs.open '{"path":"/tmp/strata-smoke-test.txt"}')
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

# 5. Revoke the capability.
echo "[5/6] Revoking capability..."
$CTL identity.revoke "{\"cap_id\":\"$CAP_ID\"}" > /dev/null
echo "  OK: capability revoked"

# 6. Verify revocation is enforced — fs.list must fail.
echo "[6/6] Verifying revocation enforcement..."
REVOKED_RESULT=$($CTL -token "$TOKEN" fs.list '{"path":"/tmp"}' 2>&1) || true
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

echo ""
echo "=== Smoke Test PASSED ==="
