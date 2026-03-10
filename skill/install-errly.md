# Skill: Install Errly

## Trigger
User wants to install, run, or deploy the Errly server.

## Agent Behavior

### 1. Check prerequisites
```bash
docker compose version   # must be v2.22+
curl --version
```

If `docker compose` is missing — detect OS and help install Docker:
```bash
uname -s   # Darwin = macOS, Linux = Linux
```

- **macOS**: direct user to https://www.docker.com/products/docker-desktop/
- **Linux (Ubuntu/Debian)**: `curl -fsSL https://get.docker.com | sh && sudo usermod -aG docker $USER`
- **Windows**: direct user to Docker Desktop with WSL 2

### 2. Clone the repository
```bash
git clone https://github.com/AsYourWish-ai/Errly.git
cd Errly
```

If directory already exists: `git pull`

### 3. Set API key (edit `.env` only — do not modify source files)

Load existing key or generate a new one:
```bash
export ERRLY_API_KEY=$(grep ERRLY_API_KEY .env 2>/dev/null | cut -d= -f2)
if [[ -z "$ERRLY_API_KEY" ]]; then
  export ERRLY_API_KEY=$(openssl rand -hex 16)
  echo "ERRLY_API_KEY=${ERRLY_API_KEY}" >> .env
fi
```

### 4. Start the server
```bash
docker compose up -d --build errly
```

Wait for health check (retry up to 15s):
```bash
for i in $(seq 1 15); do
  curl -sf http://localhost:5080/healthz && echo "Server OK" && break
  sleep 1
done
```

### 5. Run integration tests
```bash
bash test/integration.sh
```

All tests must pass before proceeding.

### 6. Build MCP image
```bash
docker build -f Dockerfile.mcp -t errly-mcp .
```

### 7. Confirm dashboard
Tell user to open **http://localhost:5080** and enter their API key in the top-right field.

---

## Quick reference

```bash
make up        # start server
make down      # stop server
make logs      # tail logs
make dev       # dev mode with auto-rebuild
make status    # show running containers
```

## Rules
- Only create or edit `.env` and `docker-compose.yml` — never modify source files
- All config goes in `.env`

## Troubleshooting

| Problem | Fix |
|---------|-----|
| Port 5080 in use | Set `ERRLY_ADDR=:5081` in `.env` |
| Unauthorized | Confirm `ERRLY_API_KEY` matches on server and requests |
| Stats show `—` | Enter API key in dashboard header field |
| `docker compose watch` not found | Upgrade Docker Desktop (needs Compose ≥ v2.22) |
| macOS network issue with MCP | Use `host.docker.internal` instead of `localhost` |
