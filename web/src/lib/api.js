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

async function request(method, path, body, opts = {}) {
  const init = { method, headers: {}, credentials: 'same-origin' }
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
  let data = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      if (res.ok) throw new ApiError(res.status, 'uventet svar fra serveren')
      throw new ApiError(res.status, text.slice(0, 200) || res.statusText)
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
  setTheme: (theme, themeMode) => request('PUT', '/me/theme', { theme, themeMode }),

  gamedata: () => request('GET', '/gamedata'),

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

  dropModel: (id) => request('GET', `/accounts/${id}/dropmodel`),
  accountPlan: (id) => request('GET', `/accounts/${id}/plan`),
  plan: (id, key) => request('GET', `/accounts/${id}/plan/${key}`),
}
