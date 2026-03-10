#!/usr/bin/env bash
set -euo pipefail

# ── Colors ────────────────────────────────────────────────────────────────────
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
NC='\033[0m'

ok()   { echo -e "${GREEN}✓${NC} $*"; }
err()  { echo -e "${RED}✗${NC} $*" >&2; }
info() { echo -e "${YELLOW}→${NC} $*"; }

echo -e "${BOLD}"
echo "  ███████╗██████╗ ██████╗ ██╗  ██╗   ██╗"
echo "  ██╔════╝██╔══██╗██╔══██╗██║  ╚██╗ ██╔╝"
echo "  █████╗  ██████╔╝██████╔╝██║   ╚████╔╝ "
echo "  ██╔══╝  ██╔══██╗██╔══██╗██║    ╚██╔╝  "
echo "  ███████╗██║  ██║██║  ██║███████╗██║   "
echo "  ╚══════╝╚═╝  ╚═╝╚═╝  ╚═╝╚══════╝╚═╝   "
echo -e "${NC}"
echo "  Lightweight self-hosted error monitoring"
echo ""

# ── Step 1: Check docker compose ──────────────────────────────────────────────
info "Checking prerequisites..."

if ! docker compose version &>/dev/null; then
  err "docker compose not found."
  echo "  Install Docker Desktop: https://docs.docker.com/get-docker/"
  exit 1
fi
ok "docker compose $(docker compose version --short)"

# ── Step 2: API key ───────────────────────────────────────────────────────────
ENV_FILE="$(pwd)/.env"

if [[ -f "$ENV_FILE" ]]; then
  # Load existing key if present
  EXISTING_KEY=$(grep -E '^ERRLY_API_KEY=' "$ENV_FILE" | cut -d= -f2- | tr -d '"' || true)
  if [[ -n "$EXISTING_KEY" ]]; then
    ERRLY_API_KEY="$EXISTING_KEY"
    ok "Using existing API key from .env"
  fi
fi

if [[ -z "${ERRLY_API_KEY:-}" ]]; then
  ERRLY_API_KEY="$(openssl rand -hex 16)"
  info "Generated new API key"
fi

# Write .env (create or update ERRLY_API_KEY line)
if [[ -f "$ENV_FILE" ]] && grep -qE '^ERRLY_API_KEY=' "$ENV_FILE"; then
  # Update existing line (works on both macOS and Linux)
  if [[ "$(uname)" == "Darwin" ]]; then
    sed -i '' "s|^ERRLY_API_KEY=.*|ERRLY_API_KEY=${ERRLY_API_KEY}|" "$ENV_FILE"
  else
    sed -i "s|^ERRLY_API_KEY=.*|ERRLY_API_KEY=${ERRLY_API_KEY}|" "$ENV_FILE"
  fi
else
  echo "ERRLY_API_KEY=${ERRLY_API_KEY}" >> "$ENV_FILE"
fi
ok "API key saved to .env"

# Export for docker compose
export ERRLY_API_KEY

# ── Step 3: Start server ──────────────────────────────────────────────────────
info "Starting Errly (this may take a minute on first run)..."
docker compose up -d --build errly

# ── Step 4: Health check ──────────────────────────────────────────────────────
info "Waiting for server to be ready..."
MAX=15
for i in $(seq 1 $MAX); do
  if curl -sf http://localhost:5080/healthz &>/dev/null; then
    ok "Server is healthy"
    break
  fi
  if [[ $i -eq $MAX ]]; then
    err "Server did not start within ${MAX}s"
    echo ""
    echo "  Check logs with:  docker compose logs errly"
    exit 1
  fi
  sleep 1
done

# ── Done ──────────────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}Errly is ready!${NC}"
echo ""
ok "Dashboard → http://localhost:5080"
ok "API key   → ${ERRLY_API_KEY}"
echo ""
echo "  Enter your API key in the top-right field of the dashboard."
echo "  To stop:    docker compose down"
echo "  To restart: docker compose up -d"
echo ""
