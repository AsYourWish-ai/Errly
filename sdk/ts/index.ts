/**
 * Errly Next.js SDK — lightweight error monitoring
 *
 * Usage (app router):
 *   // lib/errly.ts
 *   import { Errly } from './errly'
 *   export const errly = new Errly({ url: 'http://localhost:5080', apiKey: 'xxx', project: 'web' })
 *
 *   // app/global-error.tsx
 *   errly.captureError(error)
 *
 *   // API route middleware
 *   export default errly.withNextJS(handler)
 */

export interface ErrlyConfig {
  url: string
  apiKey?: string
  project?: string
  environment?: string
  release?: string
}

export interface ErrlyUser {
  id?: string
  email?: string
  username?: string
}

export interface ErrlyEvent {
  id: string
  timestamp: string
  level: string
  message: string
  exception?: { type: string; value: string }
  stacktrace?: StackFrame[]
  user?: ErrlyUser
  tags?: Record<string, string>
  extra?: Record<string, unknown>
  breadcrumbs?: Breadcrumb[]
  environment: string
  release?: string
  platform: string
  project_key: string
  request?: RequestInfo
}

interface StackFrame {
  filename: string
  function: string
  lineno: number
  colno: number
  in_app: boolean
}

interface Breadcrumb {
  timestamp: string
  type: string
  category: string
  message: string
  level: string
}

interface RequestInfo {
  url?: string
  method?: string
  headers?: Record<string, string>
}

export class Errly {
  private config: Required<ErrlyConfig>
  private user: ErrlyUser | null = null
  private breadcrumbs: Breadcrumb[] = []
  private queue: ErrlyEvent[] = []
  private flushing = false

  constructor(config: ErrlyConfig) {
    this.config = {
      url: config.url.replace(/\/$/, ''),
      apiKey: config.apiKey ?? '',
      project: config.project ?? '',
      environment: config.environment ?? 'production',
      release: config.release ?? '',
    }

    // Auto-capture unhandled errors in browser
    if (typeof window !== 'undefined') {
      this._setupBrowserHandlers()
    }
  }

  // ── Public API ─────────────────────────────────────────────────────────────

  /** Capture an Error object */
  captureError(error: Error, extra?: Record<string, unknown>): string {
    const event = this._newEvent('error')
    event.message = error.message
    event.exception = {
      type: error.name || 'Error',
      value: error.message,
    }
    event.stacktrace = parseStack(error.stack ?? '')
    if (extra) event.extra = extra
    this._send(event)
    return event.id
  }

  /** Capture a plain message */
  captureMessage(message: string, level: 'info' | 'warning' | 'error' = 'info', extra?: Record<string, unknown>): string {
    const event = this._newEvent(level)
    event.message = message
    if (extra) event.extra = extra
    this._send(event)
    return event.id
  }

  /** Set the current user */
  setUser(user: ErrlyUser | null): void {
    this.user = user
  }

  /** Add a breadcrumb */
  addBreadcrumb(message: string, category = 'default', level: 'info' | 'warning' | 'error' = 'info'): void {
    this.breadcrumbs.push({
      timestamp: new Date().toISOString(),
      type: 'default',
      category,
      message,
      level,
    })
    if (this.breadcrumbs.length > 50) {
      this.breadcrumbs = this.breadcrumbs.slice(-50)
    }
  }

  /** Next.js API route handler wrapper (Pages Router) */
  withNextJS<T>(
    handler: (req: any, res: any) => Promise<T> | T
  ): (req: any, res: any) => Promise<T> {
    return async (req: any, res: any) => {
      try {
        return await handler(req, res)
      } catch (error) {
        if (error instanceof Error) {
          const event = this._newEvent('error')
          event.message = error.message
          event.exception = { type: error.name, value: error.message }
          event.stacktrace = parseStack(error.stack ?? '')
          event.request = {
            url: req.url,
            method: req.method,
            headers: { 'user-agent': req.headers?.['user-agent'] ?? '' },
          }
          this._send(event)
        }
        throw error
      }
    }
  }

  /** Next.js App Router — wrap a Server Action or Route Handler */
  withAction<T extends (...args: any[]) => Promise<any>>(action: T): T {
    return (async (...args: Parameters<T>) => {
      try {
        return await action(...args)
      } catch (error) {
        if (error instanceof Error) {
          this.captureError(error)
        }
        throw error
      }
    }) as T
  }

  // ── Internal ───────────────────────────────────────────────────────────────

  private _newEvent(level: string): ErrlyEvent {
    return {
      id: generateId(),
      timestamp: new Date().toISOString(),
      level,
      message: '',
      platform: typeof window !== 'undefined' ? 'javascript' : 'node',
      environment: this.config.environment,
      release: this.config.release,
      project_key: this.config.project,
      user: this.user ?? undefined,
      breadcrumbs: [...this.breadcrumbs],
    }
  }

  private _send(event: ErrlyEvent): void {
    const url = `${this.config.url}/api/v1/events`
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
    }
    if (this.config.apiKey) {
      headers['X-Errly-Key'] = this.config.apiKey
    }

    // Use sendBeacon during page unload for best-effort delivery.
    // sendBeacon does not support custom headers, so we omit the API key
    // in beacon mode. Configure the server to allow unauthenticated ingest
    // (ERRLY_API_KEY unset) if you need beacon delivery with auth.
    if (typeof window !== 'undefined' && navigator.sendBeacon) {
      const blob = new Blob([JSON.stringify(event)], { type: 'application/json' })
      navigator.sendBeacon(url, blob)
      return
    }

    fetch(url, { method: 'POST', headers, body: JSON.stringify(event) }).catch(() => {
      // Silently drop network errors
    })
  }

  private _setupBrowserHandlers(): void {
    window.addEventListener('error', (e) => {
      if (e.error instanceof Error) {
        this.captureError(e.error)
      } else {
        this.captureMessage(`Uncaught: ${e.message}`, 'error')
      }
    })

    window.addEventListener('unhandledrejection', (e) => {
      const reason = e.reason
      if (reason instanceof Error) {
        this.captureError(reason)
      } else {
        this.captureMessage(`Unhandled Promise Rejection: ${String(reason)}`, 'error')
      }
    })
  }
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function parseStack(stack: string): StackFrame[] {
  if (!stack) return []

  const lines = stack.split('\n').slice(1) // skip first line (error message)
  const frames: StackFrame[] = []

  for (const line of lines) {
    // at functionName (filename:line:col)  OR  at filename:line:col
    const match = line.trim().match(/at (?:(.+?) \()?(.+?):(\d+):(\d+)\)?$/)
    if (!match) continue

    const [, fnName, filename, lineno, colno] = match
    frames.push({
      filename,
      function: fnName || '<anonymous>',
      lineno: parseInt(lineno, 10),
      colno: parseInt(colno, 10),
      in_app: isInApp(filename),
    })
  }

  return frames
}

function isInApp(filename: string): boolean {
  return (
    !filename.includes('node_modules') &&
    !filename.includes('<anonymous>') &&
    !filename.startsWith('node:')
  )
}

function generateId(): string {
  if (typeof crypto !== 'undefined' && crypto.randomUUID) {
    return crypto.randomUUID().replace(/-/g, '')
  }
  return Math.random().toString(36).slice(2) + Date.now().toString(36)
}

// ── React Error Boundary helper ───────────────────────────────────────────────

/**
 * Creates props for a React Error Boundary that captures to Errly.
 * Use with react-error-boundary or your own ErrorBoundary.
 *
 * Example:
 *   <ErrorBoundary {...errly.errorBoundaryProps()}>
 *     <App />
 *   </ErrorBoundary>
 */
export function createErrorBoundaryHandler(client: Errly) {
  return {
    onError: (error: Error, info: { componentStack: string }) => {
      client.captureError(error, { componentStack: info.componentStack })
    },
  }
}
