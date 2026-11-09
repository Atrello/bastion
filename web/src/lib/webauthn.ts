// Passkeys in the browser: turning the panel's WebAuthn options (JSON, with binary fields as base64url)
// into what navigator.credentials wants, and the browser's answer back into JSON for the panel.

const toBuffer = (b64url: string) =>
  Uint8Array.from(atob(b64url.replace(/-/g, '+').replace(/_/g, '/').padEnd(Math.ceil(b64url.length / 4) * 4, '=')), (c) =>
    c.charCodeAt(0),
  ).buffer;

const toB64url = (buffer: ArrayBuffer | null) =>
  buffer ? btoa(String.fromCharCode(...new Uint8Array(buffer))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '') : null;

type Descriptor = { id: string; type: 'public-key'; transports?: AuthenticatorTransport[] };

/** Makes a new passkey from the panel's registration options, giving the answer to send back. */
export async function createPasskey(options: { publicKey: Record<string, any> }) {
  const pk = options.publicKey;
  const credential = (await navigator.credentials.create({
    publicKey: {
      ...pk,
      challenge: toBuffer(pk.challenge),
      user: { ...pk.user, id: toBuffer(pk.user.id) },
      excludeCredentials: (pk.excludeCredentials ?? []).map((c: Descriptor) => ({ ...c, id: toBuffer(c.id) })),
    } as PublicKeyCredentialCreationOptions,
  })) as PublicKeyCredential;
  const response = credential.response as AuthenticatorAttestationResponse;
  return {
    id: credential.id,
    rawId: toB64url(credential.rawId),
    type: credential.type,
    response: {
      clientDataJSON: toB64url(response.clientDataJSON),
      attestationObject: toB64url(response.attestationObject),
      transports: response.getTransports?.() ?? [],
    },
  };
}

/** Signs in with a passkey from the panel's options, giving the answer to send back. */
export async function usePasskey(options: { publicKey: Record<string, any> }) {
  const pk = options.publicKey;
  const credential = (await navigator.credentials.get({
    publicKey: {
      ...pk,
      challenge: toBuffer(pk.challenge),
      allowCredentials: (pk.allowCredentials ?? []).map((c: Descriptor) => ({ ...c, id: toBuffer(c.id) })),
    } as PublicKeyCredentialRequestOptions,
  })) as PublicKeyCredential;
  const response = credential.response as AuthenticatorAssertionResponse;
  return {
    id: credential.id,
    rawId: toB64url(credential.rawId),
    type: credential.type,
    response: {
      clientDataJSON: toB64url(response.clientDataJSON),
      authenticatorData: toB64url(response.authenticatorData),
      signature: toB64url(response.signature),
      userHandle: toB64url(response.userHandle),
    },
  };
}
