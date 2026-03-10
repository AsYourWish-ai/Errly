# Skill: Install Errly

## Trigger
User wants to install, run, or deploy the Errly server.

## Agent Behavior

### 1. Detect environment
- Check for Docker: `docker compose version`
- Check for Go (source build): `go version`
- Identify OS (macOS / Linux / Windows)
- If tools are missing → provide install instructions before continuing

### 2. Confirm API key
- Ask user for an API key or suggest generating one
- Never proceed without setting `ERRLY_API_KEY`

### 3. Start the server

**Docker (recommended)**
```bash
ERRLY_API_KEY=your-secret-key docker compose up -d --build
```

**Dev mode (auto-rebuild on save)**
```bash
ERRLY_API_KEY=your-secret-key docker compose watch
```

**Source build**
```bash
cd src/server
go build -o errly-server .
ERRLY_API_KEY=your-secret-key ./errly-server
```

### 4. Verify health
```bash
curl http://localhost:5080/healthz
# → ok
```

### 5. Send test event
```bash
curl -X POST http://localhost:5080/api/v1/events \
  -H "Content-Type: application/json" \
  -H "X-Errly-Key: your-secret-key" \
  -d '{"level":"error","message":"install test","project_key":"test","environment":"local"}'
```

### 6. Confirm dashboard
Tell user to open **http://localhost:5080** and enter their API key in the top-right field. The test issue should appear.

### 7. Optional — build MCP image
```bash
docker build -f Dockerfile.mcp -t errly-mcp .
```

## Troubleshooting

| Problem | Fix |
|---------|-----|
| Port 5080 in use | Set `ERRLY_ADDR=:5081` in docker-compose.yml |
| Unauthorized | Confirm `ERRLY_API_KEY` matches on server and requests |
| Stats show `—` | Enter API key in dashboard header field |
| `docker compose watch` not found | Upgrade Docker Desktop (needs Compose ≥ v2.22) |
| macOS network issue with MCP | Replace `--network host` with `host.docker.internal` |
