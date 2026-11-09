# The passkey ceremony page

This folder is the browser page for a WebAuthn ceremony. It has no build step
or dependencies: `index.html`, `style.css` and `ceremony.js`. The app embeds
these files with `embed.go`.

## Supported transport

The app serves the page at `http://localhost:<random port>` and opens it in
the system browser. `localhost` is a secure context WebAuthn accepts. The
server binds `127.0.0.1` only; the relying party id (`rpId`) is `localhost`.
The page posts its result to `/result` on the same origin.

This build does **not** support a hosted HTTPS page or passkey custom-scheme
callbacks. The generic URL parsers in `internal/webauthn` are not an app
callback registration. Moving the page to a different domain also changes the
relying party and does not recover an existing localhost credential.

## Request and response

The request is carried in the fragment, which never reaches the HTTP server
or its logs. `internal/webauthn.CeremonyURL` builds it:

```
http://localhost:52341/#mode=get&challenge=...&state=...&credentialId=...
```

- `create`: `challenge`, `state`, `userId`, `userName`, optional
  `userDisplayName`, `rpName`.
- `get`: `challenge`, `state`, `credentialId`. Used for both transfer
  authorization and proof of control when importing recovery data.

Binary values use unpadded base64url. The page uses its own hostname as the
relying party id, never one supplied in the request.

The page answers with a same-origin POST:

```json
{"kind":"get","state":"...","credentialId":"...","clientDataJSON":"...","authenticatorData":"...","signature":"..."}
```

`create` carries `attestationObject` and `transports` instead of
`authenticatorData` and `signature`. An error is
`{"error":"Canceled","message":"...","state":"..."}`. Both results and
errors must match the pending state, and only one answer is accepted. The
page sends a cancellation beacon when closed before completion; the app also
cancels pending operations on lock, app-window close, removal and network
changes. A failed POST has no alternative delivery transport: restart the
operation in the app.

## Public recovery data

The app's **Export recovery file** saves a versioned JSON object containing
only `format`, `version`, `rpId`, `credentialId`, `publicKey` and
`nearAccount`. The private key remains in the authenticator. The file exposes
account identifiers, so keep it somewhere appropriate for that information.

Recovery requires **both** this file and access to the original credential
(for example its synced copy in a password manager). Import validates the
format, sizes, public key and derived NEAR address, then requests a fresh
WebAuthn assertion from that exact credential. The app verifies user presence,
user verification, credential id, challenge, origin, relying party and
signature before saving any identity. The EVM address is read again from the
factory using the verified public key.

A synced passkey alone cannot reconstruct the missing public-key/account
mapping in this build. A recovery file alone cannot sign. If the public data
is lost after forgetting the wallet, this app cannot recover the original
account from the credential alone. Creating a new passkey creates a different
wallet, not a recovery of the old one.

## Security notes

- The server serves embedded files only, caps result bodies at 128 KB and
  sends `Cache-Control: no-store`, `X-Content-Type-Options: nosniff` and
  `Referrer-Policy: no-referrer`.
- A matching state is required even for an error response. A forged result
  still has to pass all WebAuthn verification.
- No third-party scripts, fonts or images. The CSP permits only the page's
  files and same-origin POSTs.
- Registration asks for `residentKey: "required"`,
  `userVerification: "required"` and `attestation: "none"`. A discoverable
  credential still needs the public recovery data for this app's recovery
  flow.
- The relayer key is read only after assertion verification and transaction
  preparation. The app rechecks the pending operation, wallet identity,
  network and relayer before signing and broadcasting. Canceling a pending
  operation cannot reverse a transaction already submitted to the network.

## Local inspection

Wallet → **Create passkey** is the complete flow, including the result server.
For static page inspection only:

```sh
python3 -m http.server 8080 --directory web/passkey
```

Open a localhost URL built by `CeremonyURL`. A static server has no `/result`
endpoint, so it cannot complete an app ceremony or import a wallet.
