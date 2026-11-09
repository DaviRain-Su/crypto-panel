# The passkey ceremony page

This folder is the static page a browser opens for a WebAuthn ceremony. It
has no build step and no dependencies: `index.html`, `style.css` and
`ceremony.js`. The app embeds them with `embed.go`.

## Hosting

The app serves this page itself, on `http://localhost:<random port>`, and
opens that URL in the system browser. `localhost` is a secure context, so
WebAuthn accepts it, and the page's hostname is the relying party id
(`rpId`). The page posts its result back to the app's loopback server; the
server binds `127.0.0.1` only and checks the ceremony state.

A stable HTTPS host (for example `https://crypto-panel.app/passkey/`) also
works, but the page then has no `/result` endpoint and falls back to the
custom scheme below. A passkey is bound to its rpId for life, so a production
deployment needs a domain that will be kept.

## The app opens it with the request in the fragment

```
http://localhost:52341/#mode=create&challenge=...&state=...&userId=...&userName=...
```

The fragment never reaches the server or its logs.
`internal/webauthn.CeremonyURL` builds this URL.

- `create`: `challenge`, `state`, `userId`, `userName`, optional
  `userDisplayName`, `rpName`.
- `get`: `challenge`, `state`, `credentialId`.

The page uses `location.hostname` as `rp.id` / `rpId`; it never takes a
relying party from the URL. `challenge`, `userId` and `credentialId` are
base64url without padding.

## The page answers with a POST

When the page is served over `http:` or `https:`, it posts the result to
`/result` on its own origin:

```json
{"kind":"get","state":"...","credentialId":"...","clientDataJSON":"...","authenticatorData":"...","signature":"..."}
```

`create` carries `attestationObject` and `transports` instead of
`authenticatorData` and `signature`. An error is
`{"error":"Canceled","message":"...","state":"..."}`. The app's
`internal/webauthn.ParseResultJSON` reads it, and the server accepts the POST
only when its `state` matches the ceremony it started.

A page hosted elsewhere has no `/result` endpoint, so it falls back to:

```
cryptopanel://passkey?result=<base64url(JSON)>
```

`internal/webauthn.ParseCallback` reads that form. The app must register the
`cryptopanel` scheme, keep the pending `state` and challenge, and verify the
result before using it. The page is a courier: the smart account verifies the
P-256 signature on chain.

## Security notes

- The challenge and user id are in the fragment, not the query.
- The app's server binds `127.0.0.1`, serves only the embedded files, sends
  `Cache-Control: no-store`, `X-Content-Type-Options: nosniff` and
  `Referrer-Policy: no-referrer`, and caps the result body at 128 KB.
- The result's `state` must match the ceremony the app started. A forged
  result still has to pass the WebAuthn checks: the challenge, origin, rpId
  and the P-256 signature.
- No third-party scripts, fonts or images; the CSP allows only the page's own
  files and `connect-src 'self'` for the POST.
- The page's origin is the rpId. Moving the page to another domain invalidates
  existing passkeys.
- The page asks for `userVerification: "required"`, `residentKey: "required"`
  and `attestation: "none"`: a discoverable passkey, gated by the device's
  biometric or PIN, with no attestation identity leak.

## Local development

The app's own server is the normal path: Wallet → **Create passkey**. To
inspect the page by hand, serve it statically:

```sh
python3 -m http.server 8080 --directory web/passkey
```

Open `http://localhost:8080/#mode=create&challenge=...` with a valid
base64url challenge and the other fields. WebAuthn treats `localhost` as a
secure context. Use the Go helper to build the URL instead of typing it. A
statically served page uses the custom-scheme fallback, not the POST.

## Limits

A custom-scheme URL has browser-specific length limits. A platform passkey's
result is a few kilobytes, which works in current browsers; the app's own
loopback POST has no such limit.
