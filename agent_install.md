# Errly — Agent Install & Integration Test

You are an AI agent. Read this file fully, then execute every step in order.
Do not skip steps. Verify each step before moving to the next.

**Important rules:**
- You may only create or edit `.env` and `docker-compose.yml`.
- Do NOT modify any source files (`src/`, `sdk/`, `*.go`, `*.py`, `*.ts`, `Dockerfile*`, etc.).
- All configuration is done via `.env` only.

---

## Step 1 — Check prerequisites

Run the following:

```bash
docker compose version
curl --version
```

### If `docker compose` is missing — help the user install Docker

Detect the OS and guide accordingly:

```bash
uname -s   # Darwin = macOS, Linux = Linux
uname -m   # arm64 / x86_64
```

**macOS:**
```bash
# Open the Docker Desktop download page
open "https://www.docker.com/products/docker-desktop/"
```
Tell the user:
> Download and install **Docker Desktop for Mac** (choose Apple Silicon or Intel based on your chip).
> After install, open Docker Desktop and wait for it to finish starting (whale icon in menu bar turns solid).
> Then re-run this step.

**Linux (Ubuntu / Debian):**
```bash
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker $USER
newgrp docker
docker compose version
```

**Linux (other distros):**
Tell the user to follow https://docs.docker.com/engine/install/ for their distro.

**Windows:**
Tell the user:
> Download **Docker Desktop for Windows** from https://www.docker.com/products/docker-desktop/
> Enable WSL 2 when prompted during install.
> After install, open Docker Desktop and wait for it to start.
> Then re-run this step from a WSL 2 terminal.

After the user installs Docker, verify:
```bash
docker compose version   # must be v2.22+
```

If it still fails → check that Docker Desktop is running (not just installed), then retry.

---

## Step 2 — Clone the repository

```bash
git clone https://github.com/AsYourWish-ai/Errly
cd Errly
```

If the `Errly` directory already exists, pull latest instead:

```bash
cd Errly && git pull
```

All remaining steps must be run from inside the `Errly` directory.

---

## Step 3 — Set API key (edit `.env` only)

If a `.env` file already exists and contains `ERRLY_API_KEY`, load it:

```bash
export ERRLY_API_KEY=$(grep ERRLY_API_KEY .env 2>/dev/null | cut -d= -f2)
```

If it is empty, generate a new key and save it:

```bash
export ERRLY_API_KEY=$(openssl rand -hex 16)
echo "ERRLY_API_KEY=${ERRLY_API_KEY}" >> .env
```

Confirm the key is set:

```bash
echo "Using API key: $ERRLY_API_KEY"
```

---

## Step 4 — Start the server

```bash
docker compose up -d --build errly
```

Wait for the health check — retry up to 15 times with 1 second between attempts:

```bash
for i in $(seq 1 15); do
  curl -sf http://localhost:5080/healthz && echo "Server OK" && break
  echo "Waiting... ($i/15)"
  sleep 1
done
```

If the server is not healthy after 15 attempts → run `docker compose logs errly` and report the error to the user.

---

## Step 5 — Verify API access

```bash
curl -sf http://localhost:5080/api/v1/stats \
  -H "X-Errly-Key: $ERRLY_API_KEY" | cat
```

Expected: JSON with `total`, `unresolved`, `resolved`, `ignored` fields.

---

## Step 6 — Run integration test suite

The script loads `ERRLY_API_KEY` from `.env` automatically.

```bash
bash test/integration.sh
```

The suite covers:
- Health check
- Stats endpoint
- Ingest 3 test events (error, warning, multi-project)
- Verify issues created
- Filter by project, environment, search
- Resolve an issue
- Verify stats updated

Expected output: all tests pass with `✓`, exit code 0.

If any test fails → read the error line, check `docker compose logs errly`, and report to the user.

---

## Step 7 — Check the dashboard

Tell the user:
> Open **http://localhost:5080** in your browser.
> Enter your API key: `$ERRLY_API_KEY` in the top-right field.
> You should see the issues created by the integration tests.

---

## Step 8 — Build MCP image

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
        "run", "-i", "--network", "host",
        "-e", "ERRLY_URL=http://localhost:5080",
        "-e", "ERRLY_API_KEY=YOUR_API_KEY",
        "errly-mcp"
      ]
    }
  }
}
```

---

## Step 9 — Report results

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
✓ MCP image: built

Errly is ready.
```

If any step fails, stop and report which step failed with the error output.

---

## Step 10 — SDK integration test (optional)

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

Run the example:
```bash
cd example/go
go mod tidy
go run main.go
```

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

Install the SDK and run the example:
```bash
pip install errly
python3 example/python/test_sdk.py
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
```

Install and run the example:
```bash
cd example/typescript
npm install
npm test
```

Verify the issue appeared:
```bash
curl -sf "http://localhost:5080/api/v1/issues?project=ts-sdk-test" \
  -H "X-Errly-Key: $ERRLY_API_KEY" | cat
```

Expected: at least 1 issue from project `ts-sdk-test`.
