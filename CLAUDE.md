# Errly — Claude Code Instructions

## Repository
- **GitHub**: `git@github.com:AsYourWish-ai/Errly.git`
- **Remote**: `origin`
- **Default branch**: `main`

## Project Overview
Errly is a lightweight self-hosted error monitoring system written in Go, with an MCP server and TypeScript SDK.

## Git Workflow
- Always branch off `main` for new features/fixes
- Branch naming: `feat/<name>`, `fix/<name>`, `chore/<name>`, `docs/<name>`
- Commit style: `<type>: <short description>` (e.g. `feat: add retry logic`)
- Never force-push to `main`
- Never commit without explicit user request
- Always confirm before pushing to remote

## Project Structure
```
/src/
  server/    Go HTTP server (main production code)
  mcp/       MCP server (Go)
  sandbox/   E2E test scripts
/sdk/
  go/        Go SDK
  python/    Python SDK
  ts/        TypeScript SDK
docker-compose.yml
CLAUDE.md
```
- `sdk/` and `docker-compose.yml` stay at root
- All main application code lives under `src/`
- No Go/TS source files at project root

## Go Standards
- Use `go fmt` and `golangci-lint` (config: `.golangci.yml`)
- Table-driven tests preferred
- Error wrapping with `fmt.Errorf("context: %w", err)`
- No `panic` in production paths

## Docker
- Use `docker-compose.yml` for local dev
- Never commit secrets or env files

## Ports
- **All services use 5xxx range** — never revert to 8080
- Errly server: **5080** (`ERRLY_ADDR=:5080`)
- MCP connects to: `http://localhost:5080`

## Dashboard UI
- Served at `http://localhost:5080/` and `/ui`
- Single-page HTML embedded via Go `//go:embed ui.html` in `server/ui.go`
- No external dependencies — pure HTML/CSS/JS
- Features: stats bar, issue list, detail panel, resolve/ignore, search, auto-refresh

## MCP Server
- MCP server runs via Docker, not natively
- Image name: `errly-mcp` (build with `docker build -f Dockerfile.mcp -t errly-mcp .`)
- Config: `.mcp.json` uses `docker run --rm -i --network host`
- Rebuild image after any changes to `mcp/`

## Testing
- Run tests before committing
- Keep coverage meaningful, not just for metrics

## Security
- No secrets in code or commits
- Validate all external inputs at system boundaries

---

## SDK Publishing Guide

Three SDKs live under `sdk/`: **Python**, **TypeScript**, **Go**.
Always bump the version in all three before publishing a release.

---

### Python SDK → PyPI (`pip install errly`)

**Package**: `errly` | **File**: `sdk/python/pyproject.toml`

#### One-time setup
```bash
# 1. Create account at https://pypi.org/account/register/
# 2. Create API token at https://pypi.org/manage/account/token/
# 3. Save credentials
cat > ~/.pypirc <<EOF
[pypi]
  username = __token__
  password = pypi-YOUR_TOKEN_HERE
EOF

# 4. Install tools
pip3 install --user build twine
```

#### Publish
```bash
cd sdk/python

# Bump version in pyproject.toml first, then:
python3 -m build
python3 -m twine upload dist/*
```

#### Test first (TestPyPI)
```bash
python3 -m twine upload --repository testpypi dist/*
pip install --index-url https://test.pypi.org/simple/ errly
```

#### Verify
```bash
pip install errly
python3 -c "from errly import Errly; print('OK')"
```

---

### TypeScript SDK → npm (`npm install @errly/sdk`)

**Package**: `@errly/sdk` | **File**: `sdk/ts/package.json`

#### One-time setup
```bash
# 1. Create account at https://www.npmjs.com/signup
# 2. Create org "errly" at https://www.npmjs.com/org/create (for @errly scope)
# 3. Login
npm login
# Or use token: npm config set //registry.npmjs.org/:_authToken YOUR_TOKEN
```

#### Publish
```bash
cd sdk/ts

# Bump version in package.json first, then:
npm install          # install devDeps (tsup, typescript)
npm run build        # compiles to dist/ (runs automatically via prepublishOnly)
npm publish --access public
```

#### Test first (dry run)
```bash
npm publish --dry-run
```

#### Verify
```bash
npm install @errly/sdk
```

---

### Go SDK → pkg.go.dev (automatic via GitHub tag)

**Module**: `github.com/AsYourWish-ai/Errly/sdk/go` | **File**: `sdk/go/go.mod`

Go modules are published automatically — no account needed. pkg.go.dev indexes any public GitHub repo.

#### Fix module path first
The current `go.mod` uses `github.com/errly/sdk` but the repo is `AsYourWish-ai/Errly`.
Update `sdk/go/go.mod`:
```
module github.com/AsYourWish-ai/Errly/sdk/go
```
Also update all internal imports in `sdk/go/errly.go` if any.

#### Publish
```bash
# Bump version tag (use sdk/go/ prefix for sub-module tags)
git tag sdk/go/v1.0.0
git push origin sdk/go/v1.0.0
```

#### Trigger pkg.go.dev indexing
```bash
GOPROXY=proxy.golang.org go list -m github.com/AsYourWish-ai/Errly/sdk/go@v1.0.0
```

#### Users install with
```bash
go get github.com/AsYourWish-ai/Errly/sdk/go@v1.0.0
```

---

### Release Checklist (all SDKs)

- [ ] All tests pass (`go test ./...`, `npm test`, `pytest`)
- [ ] Version bumped consistently across all three SDKs
- [ ] CHANGELOG / README updated
- [ ] Git tag created: `vX.Y.Z` (repo-wide) + `sdk/go/vX.Y.Z` (Go sub-module)
- [ ] Python: `twine upload dist/*`
- [ ] TypeScript: `npm publish --access public`
- [ ] Go: tag pushed → pkg.go.dev auto-indexes
