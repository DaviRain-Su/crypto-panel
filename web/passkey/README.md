# The passkey ceremony page

This folder is the static page a browser opens for a WebAuthn ceremony. It
has no build step and no dependencies: `index.html`, `style.css` and
`ceremony.js`.

## Hosting

Serve it over HTTPS at a stable domain, for example
`https://crypto-panel.app/passkey/`. The page's hostname is the WebAuthn
relying party id (`rpId`); a passkey is bound to it for life. Choose a domain
you will keep. `http://localhost` works for development; passkeys created
there are dev-only.

The page does not need server-side code. A static host (GitHub Pages,
Cloudflare Pages, an object store) is enough. The app opens the page in the
system browser; it does not embed it.

## The app opens it with the request in the fragment

```
https://crypto-panel.app/passkey/#mode=create&challenge=...&state=...&userId=...&userName=...
```

The fragment never reaches the server or its logs.
`internal/webauthn.CeremonyURL` builds this URL.

- `create`: `challenge`, `state`, `userId`, `userName`, optional
  `userDisplayName`, `rpName`.
- `get`: `challenge`, `state`, `credentialId`.

The page uses `location.hostname` as `rp.id` / `rpId`; it never takes a
relying party from the URL. `challenge`, `userId` and `credentialId` are
base64url without padding.

## The page answers with a custom scheme

```
cryptopanel://passkey?result=<base64url(JSON)>
```

`create` result: `kind`, `state`, `credentialId`, `clientDataJSON`,
`attestationObject`, `transports`.

`get` result: `kind`, `state`, `credentialId`, `clientDataJSON`,
`authenticatorData`, `signature`, `userHandle`.

An error is:

```
cryptopanel://passkey?error=<code>&message=<text>&state=<state>
```

`internal/webauthn.ParseCallback` reads both. The app must register the
`cryptopanel` scheme, keep the pending `state` and challenge, and verify the
result before using it. The page is a courier: the smart account verifies the
P-256 signature on chain.

## Security notes

- The challenge and user id are in the fragment, not the query.
- The callback scheme is fixed; the page cannot be pointed at another URL.
- No third-party scripts, fonts or images; the CSP allows only the page's own
  files. A `<meta>` CSP cannot carry `frame-ancestors`, so the host should send
  `Content-Security-Policy: frame-ancestors 'none'` (or
  `X-Frame-Options: DENY`) as a header if the page must not be framed.
- The app must check the callback's `state`, then verify the assertion against
  the challenge, origin and rpId (`webauthn.Assertion.Verify`). A callback can
  be forged; a signature cannot.
- The page's origin is the rpId. Moving the page to another domain invalidates
  existing passkeys.
- The page asks for `userVerification: "required"`, `residentKey: "required"`
  and `attestation: "none"`: a discoverable passkey, gated by the device's
  biometric or PIN, with no attestation identity leak.

## Local development

```sh
python3 -m http.server 8080 --directory web/passkey
```

Open `http://localhost:8080/#mode=create&challenge=...` with a valid
base64url challenge and the other fields. WebAuthn treats `localhost` as a
secure context. Use the Go helper to build the URL instead of typing it.

## Limits

A custom-scheme URL has browser-specific length limits. A platform passkey's
result is a few kilobytes, which works in current browsers; if a browser
refuses it, the app can switch to a localhost HTTP callback with a random
token. The page's result format does not change.
