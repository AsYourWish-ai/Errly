# Skill: Implement SDK

## Trigger
User wants to integrate Errly error capture into their application.

## Agent Behavior

### 1. Detect project
Read the user's project files to identify:
- **Language**: Go / Python / TypeScript / JavaScript
- **Framework**: Next.js, FastAPI, Flask, Gin, Echo, Express, plain HTTP, etc.
- **Entry point**: `main.go`, `main.py`, `app.py`, `app/layout.tsx`, etc.

Always read actual files — do not assume stack from the conversation alone.

### 2. Install SDK

| Language | Command |
|----------|---------|
| Go | `go get github.com/AsYourWish-ai/Errly/sdk/go@v1.0.0` |
| Python | `pip install errly` |
| Python + FastAPI | `pip install "errly[fastapi]"` |
| Python + Flask | `pip install "errly[flask]"` |
| TypeScript / Next.js | `npm install @errly/sdk` |

**Local (monorepo) installs:**
```bash
# Go — go.mod
replace github.com/AsYourWish-ai/Errly/sdk/go => ./sdk/go

# Python
pip install -e ./sdk/python

# TypeScript — package.json
{ "dependencies": { "@errly/sdk": "file:./sdk/ts" } }
```

### 3. Collect config values
Ask the user for (or read from their env files):
- `ERRLY_URL` — default `http://localhost:5080`
- `ERRLY_API_KEY`
- `project` name
- `environment` (production / staging / development)

### 4. Initialize client

**Go**
```go
import errly "github.com/AsYourWish-ai/Errly/sdk/go"

client := errly.New(
    os.Getenv("ERRLY_URL"),
    os.Getenv("ERRLY_API_KEY"),
    errly.WithProject("my-api"),
    errly.WithEnvironment(os.Getenv("APP_ENV")),
    errly.WithRelease(os.Getenv("APP_VERSION")),
)
defer client.Flush()
```

**Python**
```python
from errly import Errly
import os

errly = Errly(
    url=os.getenv("ERRLY_URL", "http://localhost:5080"),
    api_key=os.getenv("ERRLY_API_KEY", ""),
    project="my-service",
    environment=os.getenv("APP_ENV", "production"),
)
```

**TypeScript**
```typescript
// lib/errly.ts
import { Errly } from '@errly/sdk'

export const errly = new Errly({
  url: process.env.NEXT_PUBLIC_ERRLY_URL ?? 'http://localhost:5080',
  apiKey: process.env.ERRLY_API_KEY,
  project: 'my-web-app',
  environment: process.env.NODE_ENV,
})
```

### 5. Wire error capture to framework

**Go — HTTP middleware (panic + 500 auto-capture)**
```go
http.ListenAndServe(":8080", client.Middleware(myHandler))
```

**Go — manual**
```go
if err := doSomething(); err != nil {
    client.CaptureError(ctx, err)
}
```

**Python — FastAPI**
```python
app = errly.instrument_fastapi(app)
```

**Python — Flask**
```python
app = errly.instrument_flask(app)
```

**Python — manual**
```python
try:
    risky()
except Exception as e:
    errly.capture_exception(e)
```

**Next.js — global error (App Router)**
```tsx
// app/global-error.tsx
'use client'
export default function GlobalError({ error }: { error: Error }) {
  errly.captureError(error)
  return <div>Something went wrong</div>
}
```

**Next.js — API route (Pages Router)**
```typescript
export default errly.withNextJS(async (req, res) => { ... })
```

**Next.js — Server Action (App Router)**
```typescript
const action = errly.withAction(async (data: FormData) => { ... })
```

**React — Error Boundary**
```tsx
import { createErrorBoundaryHandler } from '@errly/sdk'
<ErrorBoundary {...createErrorBoundaryHandler(errly)}>
  <App />
</ErrorBoundary>
```

### 6. Add user context & breadcrumbs (optional but recommended)
After authentication, call:
```go   client.SetUser(&errly.UserInfo{ID: "123", Email: "user@example.com"})  // Go
errly.set_user(user_id="123", email="user@example.com")                  # Python
errly.setUser({ id: '123', email: 'user@example.com' })                  // TS
```

### 7. Verify
Trigger a test error in the user's app → confirm it appears in the dashboard at **http://localhost:5080**.

Working integration test examples are in the `example/` folder:
- `example/go/main.go` — Go SDK
- `example/python/test_sdk.py` — Python SDK
- `example/typescript/test.mjs` — TypeScript SDK

## Rules
- Always use env vars — never hardcode URL or API key in source
- Always call `Flush()` / `flush()` on shutdown (Go / Python)
- Read the user's actual files before modifying them
- Do NOT modify source files — only edit user's app code and their `.env`
