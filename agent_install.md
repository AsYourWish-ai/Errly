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

## Step 11 — SDK integration test (optional)

Ask the user:

> Which SDK would you like to test?
> **1** — Go
> **2** — Python
> **3** — TypeScript / Node.js
> **4** — Skip

Then execute the matching section below.

---

### Option 1 — Go SDK

Check Go is installed:
```bash
go version   # requires 1.21+
```

Create a temp test file:
```bash
mkdir -p /tmp/errly-go-test && cat > /tmp/errly-go-test/main.go << 'EOF'
package main

import (
	"context"
	"fmt"
	errly "github.com/AsYourWish-ai/Errly/sdk/go"
)

func main() {
	client := errly.New(
		"http://localhost:5080",
		"ERRLY_API_KEY_PLACEHOLDER",
		errly.WithProject("go-sdk-test"),
		errly.WithEnvironment("test"),
	)
	defer client.Flush()

	id := client.CaptureError(context.Background(), fmt.Errorf("SDK test error from Go"))
	fmt.Println("captured event:", id)

	client.CaptureMessage(context.Background(), "info", "Go SDK integration test passed")
	fmt.Println("Go SDK test complete")
}
EOF
```

Replace the placeholder key and run:
```bash
sed -i '' "s/ERRLY_API_KEY_PLACEHOLDER/$ERRLY_API_KEY/" /tmp/errly-go-test/main.go
cd /tmp/errly-go-test
go mod init errly-test
go mod edit -replace github.com/AsYourWish-ai/Errly/sdk/go=<PATH_TO_REPO>/sdk/go
go mod tidy
go run main.go
```

Replace `<PATH_TO_REPO>` with the absolute path to the cloned Errly repo.

Verify the issue appeared:
```bash
curl -sf "http://localhost:5080/api/v1/issues?project=go-sdk-test" \
  -H "X-Errly-Key: $ERRLY_API_KEY" | cat
```

Expected: at least 1 issue from project `go-sdk-test`.

---

### Option 2 — Python SDK

Check Python is installed:
```bash
python3 --version   # requires 3.9+
```

Install the SDK and run a quick test:
```bash
pip install -e <PATH_TO_REPO>/sdk/python
```

Replace `<PATH_TO_REPO>` with the absolute path to the cloned Errly repo.

Create and run a test script:
```bash
python3 - << EOF
from errly import Errly
import os

client = Errly(
    url="http://localhost:5080",
    api_key="$ERRLY_API_KEY",
    project="python-sdk-test",
    environment="test",
)

# Capture an exception
try:
    raise ValueError("SDK test error from Python")
except Exception as e:
    event_id = client.capture_exception(e)
    print(f"captured exception: {event_id}")

# Capture a message
client.capture_message("Python SDK integration test passed", level="info")
print("Python SDK test complete")

client.flush()
EOF
```

Verify the issue appeared:
```bash
curl -sf "http://localhost:5080/api/v1/issues?project=python-sdk-test" \
  -H "X-Errly-Key: $ERRLY_API_KEY" | cat
```

Expected: at least 1 issue from project `python-sdk-test`.

---

### Option 3 — TypeScript / Node.js SDK

Check Node.js is installed:
```bash
node --version   # requires 18+
npm --version
```

Create a temp test project:
```bash
mkdir -p /tmp/errly-ts-test && cd /tmp/errly-ts-test

cat > package.json << 'EOF'
{
  "name": "errly-ts-test",
  "version": "1.0.0",
  "type": "module",
  "dependencies": {
    "@errly/sdk": "file:<PATH_TO_REPO>/sdk/ts"
  }
}
EOF
```

Replace `<PATH_TO_REPO>` with the absolute path to the cloned Errly repo, then:

```bash
npm install

cat > test.mjs << EOF
import { Errly } from '@errly/sdk'

const client = new Errly({
  url: 'http://localhost:5080',
  apiKey: '$ERRLY_API_KEY',
  project: 'ts-sdk-test',
  environment: 'test',
})

client.captureError(new Error('SDK test error from TypeScript'))
console.log('captured error')

client.captureMessage('TypeScript SDK integration test passed', 'info')
console.log('TypeScript SDK test complete')
EOF

node test.mjs
```

Verify the issue appeared:
```bash
curl -sf "http://localhost:5080/api/v1/issues?project=ts-sdk-test" \
  -H "X-Errly-Key: $ERRLY_API_KEY" | cat
```

Expected: at least 1 issue from project `ts-sdk-test`.

---

## Step 12 — Build MCP image (optional)

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

## Step 13 — Report results

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
✓ SDK test: Go / Python / TypeScript — OK  (or skipped)
✓ MCP image: built (or skipped)

Errly is ready.
```

If any step fails, stop and report which step failed with the error output.
