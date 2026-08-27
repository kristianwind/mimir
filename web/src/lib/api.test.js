import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, ApiError } from './api.js'

/**
 * The fetch wrapper is the one piece of the frontend with a failure mode that
 * has actually bitten: a body that is not JSON — a proxy's error page, a
 * gateway timeout — used to surface as a parse error, which says nothing about
 * what went wrong. Everything here pins a claim the file's own comments make.
 */

function respond({ status = 200, body = '', type = 'application/json' } = {}) {
  globalThis.fetch = vi.fn(async () => ({
    status,
    ok: status >= 200 && status < 300,
    statusText: 'Status Text',
    headers: { get: () => type },
    text: async () => body,
  }))
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('request', () => {
  it('returns null for 204 rather than trying to parse nothing', async () => {
    respond({ status: 204 })
    await expect(api.logout()).resolves.toBeNull()
  })

  it('parses a JSON body', async () => {
    respond({ body: JSON.stringify({ enabled: true, model: 'qwen3' }) })
    await expect(api.kvasirStatus()).resolves.toEqual({ enabled: true, model: 'qwen3' })
  })

  it('surfaces both halves of the server error shape', async () => {
    respond({
      status: 503,
      body: JSON.stringify({ error: 'no language model is configured', hint: 'Set MIMIR_LLM_BASE_URL.' }),
    })
    const err = await api.kvasirStatus().catch((e) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err.status).toBe(503)
    expect(err.message).toBe('no language model is configured')
    expect(err.hint).toBe('Set MIMIR_LLM_BASE_URL.')
  })

  // A proxy answering with HTML must not reach the user as "Unexpected token
  // <" — and must not reach them as the HTML either. The first two hundred
  // characters of a real error page are a doctype and a stack of conditional
  // comments: unreadable, and they do not even reveal that it was a proxy.
  it('does not turn a non-JSON failure into a parse error', async () => {
    respond({
      status: 502,
      body: '<!DOCTYPE html> <!--[if lt IE 7]> <html class="no-js ie6 oldie" lang="en-US">',
      type: 'text/html',
    })
    const err = await api.kvasirStatus().catch((e) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err.status).toBe(502)
    expect(err.message).not.toContain('DOCTYPE')
    expect(err.message).toBe('Mimir did not answer')
  })

  // A timeout is the one a big account actually hits, and it needs saying
  // plainly: the request was cut by what sits in front of Mimir, and the
  // remedy is not to reload harder.
  it('explains a gateway timeout instead of showing the proxy page', async () => {
    respond({ status: 504, body: '<!DOCTYPE html><html>gateway timeout</html>', type: 'text/html' })
    const err = await api.potential(1).catch((e) => e)
    expect(err.status).toBe(504)
    expect(err.message).toContain('took too long')
    expect(err.hint).toContain('large account')
  })

  it('reports a non-JSON success as an unexpected response', async () => {
    respond({ status: 200, body: 'not json at all' })
    const err = await api.kvasirStatus().catch((e) => e)
    expect(err.message).toBe('unexpected response from the server')
  })

  it('sends a body as JSON and keeps a blob as it is', async () => {
    respond({ body: '{}' })
    await api.saveGoal(1, { characterKey: 'RaidenShogun' })
    const [, init] = globalThis.fetch.mock.calls[0]
    expect(init.headers['Content-Type']).toBe('application/json')
    expect(init.body).toBe('{"characterKey":"RaidenShogun"}')

    const file = new Blob(['{"format":"GOOD"}'])
    await api.importGOOD(1, file)
    const [, upload] = globalThis.fetch.mock.calls[1]
    expect(upload.body).toBe(file)
    expect(upload.headers['Content-Type']).toBeUndefined()
  })
})
