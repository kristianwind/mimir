/**
 * The browser half of WebAuthn.
 *
 * Nearly all of this file is turning base64url strings into ArrayBuffers and
 * back, because the credential API speaks binary and JSON does not. It is
 * boring and it is where browser integrations usually go wrong: one field
 * left as a string and the authenticator refuses with an error that names
 * nothing.
 *
 * navigator.credentials is unavailable over plain http except on localhost,
 * so a passkey cannot be enrolled on an instance served without TLS. That is
 * the browser's rule, not ours, and the interface says so rather than
 * offering a button that cannot work.
 */

export function supported() {
  return typeof window !== 'undefined' && !!window.PublicKeyCredential
}

function toBuffer(base64url) {
  const pad = '='.repeat((4 - (base64url.length % 4)) % 4)
  const raw = atob((base64url + pad).replace(/-/g, '+').replace(/_/g, '/'))
  const out = new Uint8Array(raw.length)
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i)
  return out.buffer
}

function toBase64url(buffer) {
  const bytes = new Uint8Array(buffer)
  let s = ''
  for (const b of bytes) s += String.fromCharCode(b)
  return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

/** Decode the options the server sent for creating a credential. */
function creationOptions(publicKey) {
  return {
    ...publicKey,
    challenge: toBuffer(publicKey.challenge),
    user: { ...publicKey.user, id: toBuffer(publicKey.user.id) },
    excludeCredentials: (publicKey.excludeCredentials ?? []).map((c) => ({
      ...c,
      id: toBuffer(c.id),
    })),
  }
}

/** Decode the options for signing in. */
function requestOptions(publicKey) {
  return {
    ...publicKey,
    challenge: toBuffer(publicKey.challenge),
    allowCredentials: (publicKey.allowCredentials ?? []).map((c) => ({
      ...c,
      id: toBuffer(c.id),
    })),
  }
}

/** Encode what the authenticator produced, back into JSON. */
function encodeCreation(cred) {
  return {
    id: cred.id,
    rawId: toBase64url(cred.rawId),
    type: cred.type,
    clientExtensionResults: cred.getClientExtensionResults(),
    response: {
      clientDataJSON: toBase64url(cred.response.clientDataJSON),
      attestationObject: toBase64url(cred.response.attestationObject),
      transports: cred.response.getTransports?.() ?? [],
    },
  }
}

function encodeAssertion(cred) {
  return {
    id: cred.id,
    rawId: toBase64url(cred.rawId),
    type: cred.type,
    clientExtensionResults: cred.getClientExtensionResults(),
    response: {
      clientDataJSON: toBase64url(cred.response.clientDataJSON),
      authenticatorData: toBase64url(cred.response.authenticatorData),
      signature: toBase64url(cred.response.signature),
      userHandle: cred.response.userHandle ? toBase64url(cred.response.userHandle) : null,
    },
  }
}

/** Create a credential from the server's options. */
export async function create(options) {
  const cred = await navigator.credentials.create({ publicKey: creationOptions(options.publicKey) })
  if (!cred) throw new Error('The authenticator did not produce a passkey.')
  return encodeCreation(cred)
}

/** Sign a challenge with an existing credential. */
export async function get(options) {
  const cred = await navigator.credentials.get({ publicKey: requestOptions(options.publicKey) })
  if (!cred) throw new Error('No passkey was offered.')
  return encodeAssertion(cred)
}
