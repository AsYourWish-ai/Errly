Help the user install and run the Errly self-hosted error monitoring server.

## Your goal
Get Errly running on the user's machine and verify it is working end-to-end.

## Steps

### 1. Check prerequisites
Verify the user has the required tools:
- Docker + Docker Compose v2 (`docker compose version`)
- If building from source: Go 1.21+ (`go version`)

If anything is missing, give the exact install command for their OS.

### 2. Set the API key
Ask the user for an API key or suggest one. Then set it:
```bash
export ERRLY_API_KEY=your-secret-key
```

Or create a `.env` file:
```
ERRLY_API_KEY=your-secret-key
```

### 3. Start the server

**Docker (recommended):**
```bash
docker compose up -d --build
```

**Development mode (auto-rebuild on file change):**
```bash
docker compose watch
```

**From source:**
```bash
cd src/server
go build -o errly-server .
ERRLY_API_KEY=your-secret-key ./errly-server
```

### 4. Verify the server is running
```bash
curl http://localhost:5080/healthz
# Expected: ok
```

### 5. Open the dashboard
Tell the user to open **http://localhost:5080** in their browser and enter their API key in the top-right field.

### 6. Send a test event to confirm ingestion works
```bash
curl -X POST http://localhost:5080/api/v1/events \
  -H "Content-Type: application/json" \
  -H "X-Errly-Key: your-secret-key" \
  -d '{
    "level": "error",
    "message": "Test error from install verification",
    "project_key": "test",
    "environment": "local"
  }'
```

The dashboard should show 1 issue after refreshing.

### 7. MCP image (optional — for AI agent use)
```bash
docker build -f Dockerfile.mcp -t errly-mcp .
```

## Troubleshooting
- **Port 5080 in use** → change `ERRLY_ADDR=:5081` in docker-compose.yml
- **Unauthorized errors** → confirm `ERRLY_API_KEY` matches what you set
- **Dashboard shows 0 stats** → enter the API key in the dashboard header field
- **docker compose watch not found** → upgrade Docker Desktop (requires Compose v2.22+)
