import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { Errly, createErrorBoundaryHandler, type ErrlyEvent } from './index'

// ── fetch mock helpers ────────────────────────────────────────────────────────

function mockFetch(status = 201) {
  const captured: { url: string; options: RequestInit }[] = []
  const mock = vi.fn(async (url: string, options: RequestInit) => {
    captured.push({ url, options })
    return new Response('{"id":"ok"}', { status })
  })
  vi.stubGlobal('fetch', mock)
  return captured
}

function makeClient(overrides: Partial<ConstructorParameters<typeof Errly>[0]> = {}) {
  return new Errly({
    url: 'http://localhost:5080',
    apiKey: 'test-key',
    project: 'test-proj',
    environment: 'test',
    ...overrides,
  })
}

function parseSentEvent(captured: { url: string; options: RequestInit }[]): ErrlyEvent {
  const body = captured[0].options.body as string
  return JSON.parse(body) as ErrlyEvent
}

beforeEach(() => {
  vi.unstubAllGlobals()
})

afterEach(() => {
  vi.restoreAllMocks()
})

// ── captureError ──────────────────────────────────────────────────────────────

describe('captureError', () => {
  it('returns a non-empty event id', () => {
    mockFetch()
    const c = makeClient()
    const id = c.captureError(new Error('boom'))
    expect(id).toBeTruthy()
    expect(id).toHaveLength(32)
  })

  it('sends event with correct shape', () => {
    const captured = mockFetch()
    const c = makeClient()
    c.captureError(new Error('disk full'))

    const ev = parseSentEvent(captured)
    expect(ev.level).toBe('error')
    expect(ev.platform).toBe('node')
    expect(ev.environment).toBe('test')
    expect(ev.project_key).toBe('test-proj')
    expect(ev.exception?.type).toBe('Error')
    expect(ev.exception?.value).toBe('disk full')
    expect(ev.message).toBe('disk full')
  })

  it('parses stacktrace from error.stack', () => {
    const captured = mockFetch()
    const c = makeClient()
    const err = new Error('trace me')
    c.captureError(err)

    const ev = parseSentEvent(captured)
    expect(Array.isArray(ev.stacktrace)).toBe(true)
  })

  it('attaches extra data', () => {
    const captured = mockFetch()
    const c = makeClient()
    c.captureError(new Error('err'), { orderId: '123' })

    const ev = parseSentEvent(captured)
    expect(ev.extra?.orderId).toBe('123')
  })

  it('sends X-Errly-Key header', () => {
    const captured = mockFetch()
    const c = makeClient()
    c.captureError(new Error('err'))

    const headers = captured[0].options.headers as Record<string, string>
    expect(headers['X-Errly-Key']).toBe('test-key')
  })

  it('does not include apiKey in URL', () => {
    const captured = mockFetch()
    const c = makeClient()
    c.captureError(new Error('err'))

    expect(captured[0].url).not.toContain('key=')
    expect(captured[0].url).toBe('http://localhost:5080/api/v1/events')
  })

  it('silently ignores fetch errors', () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('network down')))
    const c = makeClient()
    expect(() => c.captureError(new Error('err'))).not.toThrow()
  })
})

// ── captureMessage ────────────────────────────────────────────────────────────

describe('captureMessage', () => {
  it('sends message with default level info', () => {
    const captured = mockFetch()
    const c = makeClient()
    c.captureMessage('Something slow')

    const ev = parseSentEvent(captured)
    expect(ev.message).toBe('Something slow')
    expect(ev.level).toBe('info')
  })

  it('respects custom level', () => {
    const captured = mockFetch()
    const c = makeClient()
    c.captureMessage('Payment failed', 'warning')

    expect(parseSentEvent(captured).level).toBe('warning')
  })

  it('returns non-empty id', () => {
    mockFetch()
    const c = makeClient()
    expect(c.captureMessage('test')).toHaveLength(32)
  })
})

// ── setUser ───────────────────────────────────────────────────────────────────

describe('setUser', () => {
  it('attaches user to subsequent events', () => {
    const captured = mockFetch()
    const c = makeClient()
    c.setUser({ id: 'u1', email: 'u@example.com', username: 'alice' })
    c.captureError(new Error('err'))

    const ev = parseSentEvent(captured)
    expect(ev.user?.id).toBe('u1')
    expect(ev.user?.email).toBe('u@example.com')
    expect(ev.user?.username).toBe('alice')
  })

  it('clears user when set to null', () => {
    const captured = mockFetch()
    const c = makeClient()
    c.setUser({ id: 'u1' })
    c.setUser(null)
    c.captureError(new Error('err'))

    expect(parseSentEvent(captured).user).toBeUndefined()
  })

  it('overrides previous user', () => {
    const captured = mockFetch()
    const c = makeClient()
    c.setUser({ id: 'u1' })
    c.setUser({ id: 'u2' })
    c.captureError(new Error('err'))

    expect(parseSentEvent(captured).user?.id).toBe('u2')
  })
})

// ── addBreadcrumb ─────────────────────────────────────────────────────────────

describe('addBreadcrumb', () => {
  it('attaches breadcrumb to next event', () => {
    const captured = mockFetch()
    const c = makeClient()
    c.addBreadcrumb('user clicked button', 'ui', 'info')
    c.captureError(new Error('err'))

    const ev = parseSentEvent(captured)
    expect(ev.breadcrumbs).toHaveLength(1)
    expect(ev.breadcrumbs![0].message).toBe('user clicked button')
    expect(ev.breadcrumbs![0].category).toBe('ui')
    expect(ev.breadcrumbs![0].level).toBe('info')
  })

  it('caps at 50 breadcrumbs keeping the last 50', () => {
    const captured = mockFetch()
    const c = makeClient()
    for (let i = 0; i < 55; i++) {
      c.addBreadcrumb(`crumb-${i}`)
    }
    c.captureError(new Error('err'))

    const crumbs = parseSentEvent(captured).breadcrumbs!
    expect(crumbs).toHaveLength(50)
    expect(crumbs[0].message).toBe('crumb-5')
    expect(crumbs[49].message).toBe('crumb-54')
  })

  it('uses default category and level', () => {
    const captured = mockFetch()
    const c = makeClient()
    c.addBreadcrumb('something')
    c.captureError(new Error('err'))

    const crumb = parseSentEvent(captured).breadcrumbs![0]
    expect(crumb.category).toBe('default')
    expect(crumb.level).toBe('info')
    expect(crumb.type).toBe('default')
  })
})

// ── withNextJS ────────────────────────────────────────────────────────────────

describe('withNextJS', () => {
  it('passes through successful handler result', async () => {
    mockFetch()
    const c = makeClient()
    const handler = c.withNextJS(async (_req: any, res: any) => {
      res.json({ ok: true })
    })
    const res = { json: vi.fn() }
    await handler({ url: '/api/test', method: 'GET', headers: {} }, res)
    expect(res.json).toHaveBeenCalledWith({ ok: true })
  })

  it('captures error and rethrows', async () => {
    const captured = mockFetch()
    const c = makeClient()
    const handler = c.withNextJS(async () => {
      throw new Error('handler exploded')
    })

    await expect(handler({ url: '/api/fail', method: 'POST', headers: {} }, {}))
      .rejects.toThrow('handler exploded')

    const ev = parseSentEvent(captured)
    expect(ev.exception?.value).toBe('handler exploded')
    expect(ev.request?.url).toBe('/api/fail')
    expect(ev.request?.method).toBe('POST')
  })

  it('does not capture non-Error throws', async () => {
    const captured = mockFetch()
    const c = makeClient()
    const handler = c.withNextJS(async () => {
      throw 'string error'  // not an Error instance
    })

    await expect(handler({}, {})).rejects.toBe('string error')
    expect(captured).toHaveLength(0)
  })
})

// ── withAction ────────────────────────────────────────────────────────────────

describe('withAction', () => {
  it('passes through successful action result', async () => {
    mockFetch()
    const c = makeClient()
    const action = c.withAction(async (x: number) => x * 2)
    expect(await action(21)).toBe(42)
  })

  it('captures error and rethrows', async () => {
    const captured = mockFetch()
    const c = makeClient()
    const action = c.withAction(async () => {
      throw new Error('action failed')
    })

    await expect(action()).rejects.toThrow('action failed')
    expect(captured).toHaveLength(1)
    expect(parseSentEvent(captured).exception?.value).toBe('action failed')
  })
})

// ── sendBeacon — key not in URL ───────────────────────────────────────────────

describe('sendBeacon (browser mode)', () => {
  it('does not append key to URL', () => {
    const beaconCalls: string[] = []
    vi.stubGlobal('window', {
      addEventListener: vi.fn(),
    })
    vi.stubGlobal('navigator', {
      sendBeacon: vi.fn((url: string) => {
        beaconCalls.push(url)
        return true
      }),
    })

    const c = makeClient()
    c.captureError(new Error('beacon test'))

    expect(beaconCalls[0]).not.toContain('key=')
    expect(beaconCalls[0]).toBe('http://localhost:5080/api/v1/events')
  })
})

// ── browser global handlers ───────────────────────────────────────────────────

describe('browser global error handlers', () => {
  it('captures window.onerror events', () => {
    const captured = mockFetch()
    const listeners: Record<string, Function> = {}
    vi.stubGlobal('window', {
      addEventListener: vi.fn((event: string, handler: Function) => {
        listeners[event] = handler
      }),
    })

    const _c = makeClient()
    const err = new Error('uncaught!')
    listeners['error']?.({ error: err })

    expect(captured).toHaveLength(1)
    expect(parseSentEvent(captured).exception?.value).toBe('uncaught!')
  })

  it('captures unhandledrejection events', () => {
    const captured = mockFetch()
    const listeners: Record<string, Function> = {}
    vi.stubGlobal('window', {
      addEventListener: vi.fn((event: string, handler: Function) => {
        listeners[event] = handler
      }),
    })

    const _c = makeClient()
    const err = new Error('rejected promise')
    listeners['unhandledrejection']?.({ reason: err })

    expect(captured).toHaveLength(1)
    expect(parseSentEvent(captured).exception?.value).toBe('rejected promise')
  })

  it('captures non-Error unhandledrejection as message', () => {
    const captured = mockFetch()
    const listeners: Record<string, Function> = {}
    vi.stubGlobal('window', {
      addEventListener: vi.fn((event: string, handler: Function) => {
        listeners[event] = handler
      }),
    })

    const _c = makeClient()
    listeners['unhandledrejection']?.({ reason: 'string rejection' })

    expect(captured).toHaveLength(1)
    expect(parseSentEvent(captured).level).toBe('error')
    expect(parseSentEvent(captured).message).toContain('string rejection')
  })
})

// ── createErrorBoundaryHandler ────────────────────────────────────────────────

describe('createErrorBoundaryHandler', () => {
  it('onError captures to Errly', () => {
    const captured = mockFetch()
    const c = makeClient()
    const handler = createErrorBoundaryHandler(c)

    handler.onError(new Error('component crash'), { componentStack: '\n  at MyComponent' })

    expect(captured).toHaveLength(1)
    expect(parseSentEvent(captured).exception?.value).toBe('component crash')
    expect(parseSentEvent(captured).extra?.componentStack).toContain('MyComponent')
  })
})

// ── event ID uniqueness ───────────────────────────────────────────────────────

describe('event IDs', () => {
  it('are unique across captures', () => {
    mockFetch()
    const c = makeClient()
    const ids = new Set<string>()
    for (let i = 0; i < 50; i++) {
      ids.add(c.captureMessage(`msg-${i}`))
    }
    expect(ids.size).toBe(50)
  })
})

// ── no apiKey — header omitted ────────────────────────────────────────────────

describe('no apiKey', () => {
  it('omits X-Errly-Key header when apiKey not set', () => {
    const captured = mockFetch()
    const c = new Errly({ url: 'http://localhost:5080', project: 'p' })
    c.captureError(new Error('err'))

    const headers = captured[0].options.headers as Record<string, string>
    expect(headers['X-Errly-Key']).toBeUndefined()
  })
})

// ── platform detection ────────────────────────────────────────────────────────

describe('platform field', () => {
  it('is "node" in Node.js environment', () => {
    const captured = mockFetch()
    const c = makeClient()
    c.captureError(new Error('err'))
    expect(parseSentEvent(captured).platform).toBe('node')
  })
})
