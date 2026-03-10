# Errly — Agent Install & Integration Test

You are an AI agent. Read this file fully, then execute every step in order.
Do not skip steps. Verify each step before moving to the next.

---

## Step 1 — Check prerequisites

Run the following and confirm each tool exists:

```bash
docker compose version   # must be v2.22+
curl --version
go version               # only needed if building from source
```

If `docker compose` is missing → tell the user to install Docker Desktop and stop.

---

## Step 2 — Set environment

Ask the user for an API key, or generate a random one:

```bash
export ERRLY_API_KEY=changeme-$(openssl rand -hex 8)
echo "Using API key: $ERRLY_API_KEY"
```

---

## Step 3 — Start the server

```bash
docker compose up -d --build
```

Wait 5 seconds, then verify the server is healthy:

```bash
curl -sf http://localhost:5080/healthz && echo "Server OK"
```

If the health check fails → run `docker compose logs errly` and report the error to the user.

---

## Step 4 — Verify API access

```bash
curl -sf http://localhost:5080/api/v1/stats \
  -H "X-Errly-Key: $ERRLY_API_KEY" | cat
```

Expected: JSON with `total`, `unresolved`, `resolved`, `ignored` fields.

---

## Step 5 — Ingest test events

Send 3 test events with different levels and projects:

```bash
# Error event
curl -sf -X POST http://localhost:5080/api/v1/events \
  -H "Content-Type: application/json" \
  -H "X-Errly-Key: $ERRLY_API_KEY" \
  -d '{
    "level": "error",
    "message": "NullPointerException in UserService.getById",
    "project_key": "backend",
    "environment": "staging",
    "exception": {
      "type": "NullPointerException",
      "value": "Cannot invoke method getById() on null object"
    },
    "stacktrace": [
      {"filename": "UserService.java", "function": "getById", "lineno": 42},
      {"filename": "UserController.java", "function": "handleRequest", "lineno": 18}
    ]
  }' && echo "Event 1 OK"

# Warning event
curl -sf -X POST http://localhost:5080/api/v1/events \
  -H "Content-Type: application/json" \
  -H "X-Errly-Key: $ERRLY_API_KEY" \
  -d '{
    "level": "warning",
    "message": "Database connection pool near capacity",
    "project_key": "backend",
    "environment": "staging"
  }' && echo "Event 2 OK"

# Error from a different project
curl -sf -X POST http://localhost:5080/api/v1/events \
  -H "Content-Type: application/json" \
  -H "X-Errly-Key: $ERRLY_API_KEY" \
  -d '{
    "level": "error",
    "message": "Unhandled promise rejection: fetch failed",
    "project_key": "frontend",
    "environment": "production"
  }' && echo "Event 3 OK"
```

---

## Step 6 — Verify issues were created

```bash
curl -sf http://localhost:5080/api/v1/issues \
  -H "X-Errly-Key: $ERRLY_API_KEY" | cat
```

Expected: `total` ≥ 3, list contains issues from `backend` and `frontend` projects.

---

## Step 7 — Test filters

```bash
# Filter by project
curl -sf "http://localhost:5080/api/v1/issues?project=backend" \
  -H "X-Errly-Key: $ERRLY_API_KEY" | cat

# Filter by environment
curl -sf "http://localhost:5080/api/v1/issues?env=production" \
  -H "X-Errly-Key: $ERRLY_API_KEY" | cat

# Search
curl -sf "http://localhost:5080/api/v1/issues/search?q=database" \
  -H "X-Errly-Key: $ERRLY_API_KEY" | cat

# Projects list
curl -sf http://localhost:5080/api/v1/projects \
  -H "X-Errly-Key: $ERRLY_API_KEY" | cat

# Environments list
curl -sf http://localhost:5080/api/v1/environments \
  -H "X-Errly-Key: $ERRLY_API_KEY" | cat
```

---

## Step 8 — Resolve an issue

Grab the first issue ID from step 6, then resolve it:

```bash
ISSUE_ID=<id from step 6>

curl -sf -X PUT "http://localhost:5080/api/v1/issues/$ISSUE_ID/status" \
  -H "Content-Type: application/json" \
  -H "X-Errly-Key: $ERRLY_API_KEY" \
  -d '{"status": "resolved"}' | cat
```

Expected: `{"status":"resolved"}` or `{"status":"resolved","removed":"true"}` if auto-remove mode is on.

---

## Step 9 — Verify stats updated

```bash
curl -sf http://localhost:5080/api/v1/stats \
  -H "X-Errly-Key: $ERRLY_API_KEY" | cat
```

Expected: `resolved` count increased by 1.

---

## Step 10 — Check the dashboard

Tell the user:
> Open **http://localhost:5080** in your browser.
> Enter your API key: `$ERRLY_API_KEY` in the top-right field.
> You should see the issues created in steps 5–8.

---

## Step 11 — Build MCP image (optional)

If the user wants AI agent access via MCP:

```bash
docker build -f Dockerfile.mcp -t errly-mcp .
```

Verify:
```bash
docker images | grep errly-mcp
```

Tell the user to add this to their `.mcp.json` or Claude Desktop config:

```json
{
  "mcpServers": {
    "errly": {
      "command": "docker",
      "args": [
        "run", "--rm", "-i", "--network", "host",
        "-e", "ERRLY_URL=http://localhost:5080",
        "-e", "ERRLY_API_KEY=YOUR_API_KEY",
        "errly-mcp"
      ]
    }
  }
}
```

---

## Step 12 — Report results

When all steps pass, output a summary like:

```
✓ Server running at http://localhost:5080
✓ API key: <key>
✓ Events ingested: 3
✓ Issues created: 3 (backend: 2, frontend: 1)
✓ Filter by project: OK
✓ Filter by environment: OK
✓ Search: OK
✓ Resolve issue: OK
✓ Stats updated: OK
✓ Dashboard: http://localhost:5080
✓ MCP image: built (optional)

Errly is ready.
```

If any step fails, stop and report which step failed with the error output.
