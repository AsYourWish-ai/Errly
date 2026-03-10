Help the user integrate the Errly SDK into their application.

## Your goal
Detect the user's language/framework, install the correct Errly SDK, and add working error capture code to their project.

## Step 1 — Detect the project
Look at the user's project files to determine:
- Language: Go / Python / TypeScript / JavaScript
- Framework: Next.js, FastAPI, Flask, Gin, Echo, plain HTTP, etc.
- Where the app entry point is (`main.go`, `main.py`, `app.py`, `app/layout.tsx`, etc.)

Read relevant files before suggesting anything.

## Step 2 — Install the SDK

### Go
```bash
go get github.com/AsYourWish-ai/Errly/sdk/go@v1.0.0
```
Local monorepo:
```
# go.mod
replace github.com/AsYourWish-ai/Errly/sdk/go => ./sdk/go
```

### Python
```bash
pip install errly
# FastAPI project:
pip install "errly[fastapi]"
# Flask project:
pip install "errly[flask]"
```
Local install:
```bash
pip install -e ./sdk/python
```

### TypeScript / Next.js
```bash
npm install @errly/sdk
```
Local monorepo:
```json
{ "dependencies": { "@errly/sdk": "file:./sdk/ts" } }
```

## Step 3 — Initialize the client

Ask the user for:
- Errly server URL (default: `http://localhost:5080`)
- API key (`ERRLY_API_KEY`)
- Project name (e.g. `"my-api"`)
- Environment (e.g. `"production"`, `"staging"`)

### Go
```go
import errly "github.com/AsYourWish-ai/Errly/sdk/go"

client := errly.New(
    os.Getenv("ERRLY_URL"),      // http://localhost:5080
    os.Getenv("ERRLY_API_KEY"),
    errly.WithProject("my-api"),
    errly.WithEnvironment(os.Getenv("APP_ENV")),
    errly.WithRelease(os.Getenv("APP_VERSION")),
)
defer client.Flush()
```

### Python
```python
from errly import Errly
import os

errly_client = Errly(
    url=os.getenv("ERRLY_URL", "http://localhost:5080"),
    api_key=os.getenv("ERRLY_API_KEY", ""),
    project="my-service",
    environment=os.getenv("APP_ENV", "production"),
)
```

### TypeScript / Next.js
```typescript
// lib/errly.ts
import { Errly } from '@errly/sdk'

export const errly = new Errly({
  url: process.env.NEXT_PUBLIC_ERRLY_URL ?? 'http://localhost:5080',
  apiKey: process.env.ERRLY_API_KEY,
  project: 'my-web-app',
  environment: process.env.NODE_ENV,
  release: process.env.NEXT_PUBLIC_RELEASE,
})
```

## Step 4 — Add error capture

Wire into the framework's error handling:

### Go — HTTP middleware
```go
http.ListenAndServe(":8080", client.Middleware(myHandler))
```

### Go — manual capture
```go
if err := doSomething(); err != nil {
    client.CaptureError(ctx, err)
}
```

### Python — FastAPI
```python
app = errly_client.instrument_fastapi(app)
```

### Python — Flask
```python
app = errly_client.instrument_flask(app)
```

### Python — manual
```python
try:
    risky()
except Exception as e:
    errly_client.capture_exception(e)
```

### Next.js — global error boundary
```tsx
// app/global-error.tsx
'use client'
export default function GlobalError({ error }: { error: Error }) {
  errly.captureError(error)
  return <div>Something went wrong</div>
}
```

### Next.js — API route (Pages Router)
```typescript
export default errly.withNextJS(async (req, res) => {
  res.json({ ok: true })
})
```

### Next.js — Server Action (App Router)
```typescript
const myAction = errly.withAction(async (data: FormData) => {
  // errors auto-captured
})
```

## Step 5 — Verify
Ask the user to trigger a test error, then check the dashboard at **http://localhost:5080** to confirm the issue appears.

## Tips
- Always use env vars for `ERRLY_URL` and `ERRLY_API_KEY` — never hardcode
- Add `errly.SetUser` / `set_user` / `setUser` after authentication to link errors to users
- Use `AddBreadcrumb` / `add_breadcrumb` / `addBreadcrumb` before risky operations for better context
