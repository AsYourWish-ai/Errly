#!/usr/bin/env bash
# Errly Integration Test Suite
# Usage: bash test/integration.sh (loads ERRLY_API_KEY from .env automatically)

# Auto-load .env from repo root (works whether run from root or test/)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${SCRIPT_DIR}/../.env"
if [[ -f "$ENV_FILE" ]]; then
  while IFS='=' read -r key value; do
    [[ -z "$key" || "$key" == \#* ]] && continue
    export "$key=$value"
  done < "$ENV_FILE"
fi

BASE_URL="${ERRLY_URL:-http://localhost:5080}"
API_KEY="${ERRLY_API_KEY:-}"

# ── Colors ─────────────────────────────────────────────────────────────────────
GREEN='\033[0;32m'; RED='\033[0;31m'; YELLOW='\033[1;33m'; BOLD='\033[1m'; NC='\033[0m'

PASS=0
FAIL=0

pass() { echo -e "${GREEN}✓${NC} $*"; PASS=$((PASS + 1)); }
fail() { echo -e "${RED}✗${NC} $*"; FAIL=$((FAIL + 1)); }
info() { echo -e "${YELLOW}→${NC} $*"; }

# ── Helpers ────────────────────────────────────────────────────────────────────
api() {
  local path="$1"; shift
  curl -sf "${BASE_URL}${path}" -H "X-Errly-Key: ${API_KEY}" "$@"
}

assert_contains() {
  local label="$1" haystack="$2" needle="$3"
  if echo "$haystack" | grep -qi "$needle"; then
    pass "$label"
  else
    fail "$label — expected to find '${needle}' in response"
  fi
}

# ── Preflight ──────────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}Errly Integration Tests${NC}"
echo "  URL: ${BASE_URL}"
echo ""

if [[ -z "$API_KEY" ]]; then
  echo -e "${RED}Error: ERRLY_API_KEY is not set${NC}"
  echo "  Create .env with: ERRLY_API_KEY=<your-key>"
  echo "  Or export it:     export ERRLY_API_KEY=<your-key>"
  exit 1
fi

# ── Test 1: Health check ───────────────────────────────────────────────────────
info "Health check"
HEALTH=$(curl -s -o /dev/null -w "%{http_code}" "${BASE_URL}/healthz")
if [[ "$HEALTH" == "200" ]]; then
  pass "Server is healthy"
else
  fail "Server health check failed (HTTP ${HEALTH})"
  echo "  Is the server running? Try: docker compose up -d errly"
  echo "  Check logs:              docker compose logs errly"
  exit 1
fi

# ── Test 2: Stats endpoint ─────────────────────────────────────────────────────
info "Stats endpoint"
STATS=$(api /api/v1/stats)
assert_contains "Stats has total field"      "$STATS" '"total"'
assert_contains "Stats has unresolved field" "$STATS" '"unresolved"'
assert_contains "Stats has resolved field"   "$STATS" '"resolved"'

# ── Test 3: Ingest events ──────────────────────────────────────────────────────
info "Ingesting test events"

RES=$(curl -s -X POST "${BASE_URL}/api/v1/events" \
  -H "Content-Type: application/json" \
  -H "X-Errly-Key: ${API_KEY}" \
  -d '{
    "level": "error",
    "message": "NullPointerException in UserService.getById",
    "project_key": "test-backend",
    "environment": "staging",
    "exception": {
      "type": "NullPointerException",
      "value": "Cannot invoke method getById() on null object"
    },
    "stacktrace": [
      {"filename": "UserService.java", "function": "getById", "lineno": 42},
      {"filename": "UserController.java", "function": "handleRequest", "lineno": 18}
    ]
  }')
assert_contains "Event 1 (error) ingested" "$RES" '"id"'

RES=$(curl -s -X POST "${BASE_URL}/api/v1/events" \
  -H "Content-Type: application/json" \
  -H "X-Errly-Key: ${API_KEY}" \
  -d '{
    "level": "warning",
    "message": "Database connection pool near capacity",
    "project_key": "test-backend",
    "environment": "staging"
  }')
assert_contains "Event 2 (warning) ingested" "$RES" '"id"'

RES=$(curl -s -X POST "${BASE_URL}/api/v1/events" \
  -H "Content-Type: application/json" \
  -H "X-Errly-Key: ${API_KEY}" \
  -d '{
    "level": "error",
    "message": "Unhandled promise rejection: fetch failed",
    "project_key": "test-frontend",
    "environment": "production"
  }')
assert_contains "Event 3 (error, different project) ingested" "$RES" '"id"'

# ── Test 4: Issues created ─────────────────────────────────────────────────────
info "Verifying issues"
ISSUES=$(api /api/v1/issues)
TOTAL=$(echo "$ISSUES" | grep -o '"total":[0-9]*' | cut -d: -f2)
if [[ "${TOTAL:-0}" -ge 3 ]]; then
  pass "Issues created (total=${TOTAL})"
else
  fail "Expected total ≥ 3, got ${TOTAL:-0}"
fi

# ── Test 5: Filters ────────────────────────────────────────────────────────────
info "Testing filters"

BACKEND=$(api "/api/v1/issues?project=test-backend")
assert_contains "Filter by project=test-backend" "$BACKEND" '"test-backend"'

PROD=$(api "/api/v1/issues?env=production")
assert_contains "Filter by env=production" "$PROD" '"production"'

SEARCH=$(api "/api/v1/issues/search?q=database")
assert_contains "Search for 'database'" "$SEARCH" 'database'

PROJECTS=$(api /api/v1/projects)
assert_contains "Projects list contains test-backend" "$PROJECTS" 'test-backend'

ENVS=$(api /api/v1/environments)
assert_contains "Environments list contains staging" "$ENVS" 'staging'

# ── Test 6: Resolve an issue ───────────────────────────────────────────────────
info "Resolving an issue"
ISSUE_ID=$(api /api/v1/issues | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)

if [[ -z "$ISSUE_ID" ]]; then
  fail "Could not get issue ID to resolve"
else
  RESOLVE=$(curl -s -X PUT "${BASE_URL}/api/v1/issues/${ISSUE_ID}/status" \
    -H "Content-Type: application/json" \
    -H "X-Errly-Key: ${API_KEY}" \
    -d '{"status": "resolved"}')
  assert_contains "Issue resolved" "$RESOLVE" '"resolved"'
fi

# ── Test 7: Stats updated ──────────────────────────────────────────────────────
info "Verifying stats updated"
STATS2=$(api /api/v1/stats)
RESOLVED=$(echo "$STATS2" | grep -o '"resolved":[0-9]*' | cut -d: -f2)
if [[ "${RESOLVED:-0}" -ge 1 ]]; then
  pass "Resolved count updated (resolved=${RESOLVED})"
else
  fail "Expected resolved ≥ 1, got ${RESOLVED:-0}"
fi

# ── Summary ────────────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}Results: ${GREEN}${PASS} passed${NC}, ${RED}${FAIL} failed${NC}"
echo ""

if [[ "$FAIL" -gt 0 ]]; then
  exit 1
fi
