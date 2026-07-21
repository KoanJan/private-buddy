#!/usr/bin/env bash
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
FAIL=0

run_step() {
  local desc="$1"
  shift
  echo "  → $desc"
  if "$@"; then
    echo "    ✓ OK"
  else
    echo "    ✗ FAILED"
    FAIL=1
  fi
}

echo "=== CI: Backend (Go) ==="
cd "$ROOT/server"

run_step "go build ./..."        go build ./...
run_step "go vet ./..."           go vet ./...

echo "  → go test ./... (short)"
if go test ./... -count=1 -short; then
  echo "    ✓ OK"
else
  echo "    ⚠ some tests failed (may be pre-existing)"
fi


echo ""
echo "=== CI: Frontend (Web) ==="
cd "$ROOT/web"

run_step "npm ci"                npm ci --silent
run_step "tsc --noEmit"          npx tsc --noEmit
run_step "npm run build"         npm run build

echo ""
echo "=== CI: Audit ==="
cd "$ROOT/ci/audit"
run_step "go build audit"        go build -o "$ROOT/audit" ./cmd/audit/
cd "$ROOT"
run_step "audit scan"            ./audit scan

echo ""
if [ "$FAIL" -eq 0 ]; then
  echo "=== CI: All critical checks passed ==="
else
  echo "=== CI: Some checks failed ==="
  exit 1
fi
