/**
 * Thin fetch wrapper.
 *
 * The server answers every failure with {error, hint}; this surfaces both so
 * the UI can show the remediation rather than a bare status code. A 401 is
 * special-cased because it always means the same thing: the session lapsed
 * and the app should fall back to the login screen.
 */

export class ApiError extends Error {
  constructor(status, message, hint) {
    super(message)
    this.status = status
    this.hint = hint
  }
}

// gatewayError explains a reply that never reached Mimir, or never came back
// from it. These come from whatever sits in front of the server, so the body
// is somebody else's error page and only the status is ours to read.
function gatewayError(res) {
  switch (res.status) {
    case 502:
    case 503:
      return new ApiError(
        res.status,
        'Mimir did not answer',
        'The server may be restarting. Give it a moment and try again.',
      )
    case 504:
    case 524:
      return new ApiError(
        res.status,
        'the answer took too long and the connection was cut',
        'This is the proxy in front of Mimir giving up, not Mimir failing. ' +
          'A large account takes longer to measure — try one character at a time, ' +
          'or ask again in a moment.',
      )
    default:
      return new ApiError(
        res.status,
        res.ok ? 'unexpected response from the server' : `the server answered ${res.status}`,
      )
  }
}

async function request(method, path, body, opts = {}) {
  const init = {
    method,
    headers: {},
    credentials: 'same-origin',
  }
  if (body !== undefined) {
    if (body instanceof Blob || typeof body === 'string') {
      init.body = body
    } else {
      init.headers['Content-Type'] = 'application/json'
      init.body = JSON.stringify(body)
    }
  }
  if (opts.signal) init.signal = opts.signal

  const res = await fetch(`/api${path}`, init)
  if (res.status === 204) return null

  const text = await res.text()

  // A non-JSON body is possible — a proxy error page, a panic recovered
  // upstream — and parsing it blind turns a plain 502 into a syntax error
  // that says nothing about what went wrong.
  //
  // What it must not do is show that body. A gateway timeout arrives as a
  // full HTML page, and the first 200 characters of one are a doctype and a
  // pile of conditional comments: the reader learns nothing and cannot even
  // tell it is a timeout. The status is the only part that carries meaning,
  // so the status is what gets explained.
  let data = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      throw gatewayError(res)
    }
  }

  if (!res.ok) {
    throw new ApiError(res.status, data?.error ?? res.statusText, data?.hint)
  }
  return data
}

export const api = {
  me: () => request('GET', '/me'),
  bootstrapStatus: () => request('GET', '/auth/bootstrap'),
  bootstrap: (username, password) => request('POST', '/auth/bootstrap', { username, password }),
  login: (username, password) => request('POST', '/auth/login', { username, password }),
  logout: () => request('POST', '/auth/logout'),
  setPrefs: (theme, themeMode) => request('PUT', '/me/prefs', { theme, themeMode }),

  gamedata: () => request('GET', '/gamedata'),

  // Kvasir, the AI layer. Optional: the status endpoint answers
  // {enabled:false} when no model is configured, and the UI leaves the whole
  // feature out rather than showing cards that can only apologise.
  kvasirStatus: () => request('GET', '/kvasir'),
  kvasirCheck: () => request('POST', '/system/kvasir/check'),
  kvasirOpinion: (id, body) => request('POST', `/accounts/${id}/kvasir/opinion`, body),
  kvasirChat: (id, body) => request('POST', `/accounts/${id}/kvasir/chat`, body),

  // What a character should aim for, whether or not you own any of it.
  target: (id, key) => request('GET', `/accounts/${id}/target/${encodeURIComponent(key)}`),

  // Somebody else's published showcase, measured the same way as yours.
  compare: (id, uid) => request('GET', `/accounts/${id}/compare/${encodeURIComponent(uid)}`),

  system: () => request('GET', '/system'),
  checkUpdate: () => request('POST', '/system/update/check'),
  applyUpdate: () => request('POST', '/system/update'),
  rollback: () => request('POST', '/system/rollback'),
  users: () => request('GET', '/users'),
  createUser: (user) => request('POST', '/users', user),
  updateUser: (id, patch) => request('PUT', `/users/${id}`, patch),
  deleteUser: (id) => request('DELETE', `/users/${id}`),
  changePassword: (current, next) => request('PUT', '/me/password', { current, new: next }),

  mineStatus: () => request('GET', '/system/gamedata/mine'),
  startMine: (version) => request('POST', '/system/gamedata/mine', { version }),

  receiver: () => request('GET', '/system/beacon/receiver'),
  setReceiver: (enabled) => request('PUT', '/system/beacon/receiver', { enabled }),

  setBeacon: (enabled, url) => request('PUT', '/system/beacon', url === undefined ? { enabled } : { enabled, url }),

  accounts: () => request('GET', '/accounts'),
  addAccount: (uid) => request('POST', '/accounts', { uid }),
  deleteAccount: (id) => request('DELETE', `/accounts/${id}`),

  importEnka: (id) => request('POST', `/accounts/${id}/import/enka`),
  importGOOD: (id, file) => request('POST', `/accounts/${id}/import/good`, file),

  characters: (id) => request('GET', `/accounts/${id}/characters`),
  artifacts: (id) => request('GET', `/accounts/${id}/artifacts`),
  weapons: (id) => request('GET', `/accounts/${id}/weapons`),
  talents: (id, key) => request('GET', `/accounts/${id}/talents/${key}`),
  build: (id, key) => request('GET', `/accounts/${id}/build/${key}`),

  goals: (id) => request('GET', `/accounts/${id}/goals`),
  saveGoal: (id, goal) => request('PUT', `/accounts/${id}/goals`, goal),
  deleteGoal: (id, key) => request('DELETE', `/accounts/${id}/goals/${key}`),

  potential: (id) => request('GET', `/accounts/${id}/potential`),
  deriveGoals: (id, body) => request('POST', `/accounts/${id}/goals/derive`, body),

  dropModel: (id) => request('GET', `/accounts/${id}/dropmodel`),
  accountPlan: (id) => request('GET', `/accounts/${id}/plan`),
  plan: (id, key) => request('GET', `/accounts/${id}/plan/${key}`),
}
