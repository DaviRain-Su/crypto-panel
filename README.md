# crypto-panel

**A chain-agnostic desktop shell that installs apps — with keys and approvals kept local.**

NEAR is the first chain it can truly sign on and connect to web apps: a native
NEAR wallet whose ed25519 keys live in the OS keychain, a shell-drawn approval
for every transfer, function call and access key, an isolated web view with an
injected NEAR wallet bridge, and a dry-run NEAR Intents quote shown in the
language of the result.

Design: [`docs/design.md`](docs/design.md). License: [MIT](LICENSE).

**中文简介**：crypto-panel 是一个 Go + MyGo 写成的原生桌面壳：插件是沙箱里的
JavaScript，页面是数据，钥匙留在系统钥匙串，每一次签名都由壳自己解码并弹窗确认。
这次黑客松把 NEAR 接成第一条能真正签名、能连接网页应用的链：测试网 NEAR 钱包、
原生 NEAR 批准窗、受限的 function-call access key、隔离 WebView 里的
`window.nearWallet` 桥，以及只跑 `dry: true` 的 NEAR Intents 报价。Passkey
尚未接线，README 末尾如实说明。

## The hackathon slice

- **Tracks:** `security` (main narrative) and `defi` (secondary).
- **Baseline:** tag [`pre-hackathon`](https://github.com/DaviRain-Su/crypto-panel/releases/tag/pre-hackathon)
  at `ccde05d` — the EVM + Solana read-only shell, the sandboxed plugin runtime
  and signed store, and the native EVM wallet with decoded approvals and a
  system (Touch ID) check.
- **This slice:** [`pre-hackathon...hackathon-submission`](https://github.com/DaviRain-Su/crypto-panel/compare/pre-hackathon...hackathon-submission)
  — NEAR testnet wallet, native NEAR approvals, function-call key limits, the
  isolated DApp web view and bridge, plugin NEAR requests, and a NEAR Intents
  dry quote. The `hackathon-submission` tag is created at submission; the link
  works once the repository is public and both tags are pushed.

What the slice deliberately does **not** do: no mnemonic/HD wallet, no Solana
signing, no Phoenix/Hyperliquid, no Chain Signatures, no mainnet Intents
without the user's explicit consent, no loosening of the plugin sandbox, and
no auto-approval. The Touch ID prompt is a system check, **not** a Passkey.

## What it is

- **The shell** (Go + [MyGo](https://github.com/egoist/mygo) native UI): a
  normal desktop window with a sidebar of pages — Portfolio, Wallet, Apps,
  Intents, DApp, Settings — plus the app launcher, permission prompts, decoded
  approvals and chain brokers.
- **Apps are plugins** (JavaScript, anyone's): a folder with `plugin.json` and
  one module, run each in its own [moejs](https://github.com/MoeForge/moejs)
  runtime — pure Go, one realm each, with memory and time limits — and only the
  host functions its manifest grants. Installed apps sit on the Apps page as a
  launcher: click one to open it, or open its settings, logs and switch. An app
  installs from a folder, a local zip, or the store, with its permissions
  shown before anything is unpacked.
- **Plugin UI is data.** A plugin returns pages (`stats`, `table`, `cards`,
  `links`, `chart`); the shell draws them with native UI. No HTML, no
  plugin-drawn dialogs, so an approval prompt cannot be imitated.
- **DApps are untrusted pages.** The DApp page opens an https URL (or http on
  localhost) in an isolated web view with a small injected NEAR wallet bridge.
  The page can ask to connect and to sign; it cannot read a key, call a Go
  method, open a window, download a file, or draw an approval.
- **The wallet is native.** EVM and NEAR accounts are created or imported on
  the Wallet page; their keys live in the OS keychain and never reach a plugin
  or a web page. Signing is only ever the shell's own decoded approval.

## What works now

### NEAR wallet (testnet by default)

- **Create an implicit account**: a fresh ed25519 key, stored in the OS
  keychain under `near:key:<accountId>`. The account ID is the 64-hex public
  key, as NEAR defines it. The key is shown once so it can be backed up, and
  again on **Reveal key** while the account is unlocked.
- **Import a key**: an `ed25519:<base58>` secret key, with an optional named
  account ID (`alice.testnet`). A named account must be typed; the key alone
  does not name it.
- **Balance**: the NEAR balance is read from the selected NEAR network and
  joins the portfolio total. **Send NEAR** opens the shell's approval window,
  then signs and broadcasts; the transaction hash is shown with a link to the
  network's explorer (nearblocks).
- **Networks**: NEAR Testnet (`https://rpc.testnet.near.org`) and NEAR Mainnet
  (`https://rpc.mainnet.near.org`). Testnet is the default for the demo. A
  custom RPC endpoint can be set in Settings and is pinned to the selected
  network.
- **Locking**: the wallet locks when the screen locks, when the window closes,
  and after five minutes idle. A locked account cannot sign; the approval
  window never promises a signature the shell cannot make.

### Native NEAR approval

Every NEAR signature goes through a modal window the shell draws itself. It
shows the shell's own decode, not the source's words:

- network and signing account;
- receiver / contract;
- the signing key and its **on-chain limit** (full access, or the
  function-call key's allowance, receiver and method list);
- for a transfer: the amount in NEAR **and** the exact yoctoNEAR value;
- for a function call: method, arguments, gas, deposit;
- for an added key: the public key, contract, method list and allowance;
- for an unknown action: its type, never a guess;
- where the request came from: a plugin's name, a DApp's origin, or the
  wallet itself.

**Reject**, closing the window, or a locked account ends the request without
signing or broadcasting. A rejected request is an error the plugin or page
sees. There is no auto-approve path.

### Function-call access keys

A NEAR access key can be limited to one contract, a method list and a
yoctoNEAR allowance. The shell enforces that limit **before** the approval
window: if the wallet's signing key is a function-call key, a transfer, an
unauthorized method, another contract, or a deposit above the allowance is
refused with a visible error and never reaches the key. The add-key approval
shows the limit the user is about to grant. The full-access key never leaves
the shell's keychain; plugins and web pages never see any key.

### DApp mode

- **Isolation**: each DApp is an untrusted `mygo.WebView` page. It has no Go
  methods, no keychain, no filesystem, no permission grants, no `window.open`,
  and no downloads. It cannot draw or dismiss an approval.
- **The bridge**: a preload script defines `window.nearWallet` with
  `connect`, `signIn`, `getAccounts`, `getNetwork`,
  `signAndSendTransaction` and `addFunctionCallKey`. Requests go to the
  shell's `cryptopanel://near/request` scheme; answers come back as an event
  because an untrusted page cannot read a cross-origin response.
- **Origin**: the origin is the page's actual URL, never a field of the
  request, so one page cannot claim to be another. Only `https`, or `http` on
  loopback, is accepted. The user approves each origin's connection once; a
  page that is not connected cannot sign.
- **A test page** lives in [`web/dapp`](web/dapp/index.html): connect, send
  NEAR, and add a function-call key. It is a demonstration of the bridge, not
  a production wallet.

### Plugin NEAR requests

A plugin with `permissions.sign` and a NEAR chain in `permissions.chains` can
call `host.requestNearSign`:

```js
const result = await host.requestNearSign({
  chainId: "near:testnet",
  receiver: "bob.testnet",
  description: "Pay for the report",
  actions: [{ type: "transfer", amount: "1000000000000000000000000" }],
});
```

The request is strictly parsed (unknown keys and action types are refused),
paused on the plugin's watchdog while the approval is open, and signed only
after the user approves. Without `sign`, or with another chain, the host
refuses before the request reaches the wallet.

### Apps and the plugin store

The Apps page is the shell's launcher. Every installed plugin is an app there:
its mark, name, version and description, with **Open** and **App settings**.
**Open** shows the app's first page, with the app's own tabs when it declares
more than one; **All apps** in the header comes back. Settings holds the
enable switch, the app's pages, its settings form, its log and **Remove**. A
folder whose `plugin.json` cannot be read shows as broken, with the error and
a **Remove** button.

An app installs three ways:

- **Install from folder…** copies a plugin folder into the store;
- **Install from zip…** unpacks a local archive — a single top-level folder is
  stripped, so a GitHub archive works as it comes;
- **Store** reads the registry URL set in Settings, with a search box over
  names, ids, authors and descriptions, and installs a signed entry after the
  permission prompt.

Local installs are unsigned and the app says so. See
[`docs/store.md`](docs/store.md) for the registry format and signing.

### NEAR Intents (dry quote)

The Intents page uses the official
[1Click Go SDK](https://github.com/defuse-protocol/one-click-sdk-go) to list
the supported assets and to ask for a **`dry: true`** quote. A dry quote
prices the swap and issues no deposit address, so no funds can move and
nothing is signed. The approval window shows the result in the user's
language:

- **You pay** — the origin amount and asset;
- **You receive at least** — the minimum output after slippage;
- **Deadline** and **deposit address** (a dry quote issues neither, and the
  window says so);
- **Refund to** and **recipient**;
- the **intent text** as one sentence;
- **Requested by** — `Crypto Panel · dry run`.

Accepting a dry quote records the conditions and says plainly that nothing
was signed or sent. Rejecting it changes nothing. A real swap (a wet quote,
a deposit, a solver execution) is deliberately **not** implemented in this
build; it would need the user's explicit consent, a separate funded
development account, and a much smaller amount. NEAR Intents has no testnet.

### Passkey: status, not a claim

The Touch ID / Face ID prompt in the wallet is a **system presence check**
(LocalAuthentication), not a WebAuthn Passkey. The signing key is still an
ed25519 or secp256k1 key in the OS keychain. Calling that key a Passkey would
be wrong, and this README does not.

The repository contains the pieces for a real passkey account:
`internal/webauthn` parses and verifies the WebAuthn ceremonies,
`internal/smartaccount` builds the ERC-4337 account and the `WebAuthnAuth`
signature, and [`web/passkey`](web/passkey/README.md) is the browser ceremony
page. They are **not wired into the app**: that needs a stable relying-party
domain, a bundler, and the account UI. Until then, the app does not claim
Passkey support.

If and when it is wired, the recovery rules are these, and they are not
optional:

- **A synced passkey** (iCloud Keychain / Apple Passwords, or Google Password
  Manager) follows the same account to another device signed in to the same
  Apple or Google account. If every device is lost, recovery goes through the
  provider's own process (for Apple: Apple Account, a trusted phone and the
  device passcode, with a limited number of attempts).
- **A device-bound passkey** (for example, one Chrome profile or Windows
  Hello) usually cannot be recovered if the device is lost.
- A NEAR account can hold several keys. The safer setup is a passkey for daily
  signing **plus a different recovery method** — another device's passkey, or
  an offline recovery key. One unsynced passkey alone is a single point of
  failure.
- A passkey's private key **cannot be exported** and re-imported like a
  mnemonic.

## Try it: step by step

Requirements: macOS (the native UI and the system check), Go 1.27.3, and
network access to the NEAR testnet RPC and 1Click.

1. **Run the app.**

   ```sh
   go run .
   ```

2. **Switch to NEAR Testnet.** Settings → Chain type → NEAR → Chain → NEAR
   Testnet. The sidebar footer shows the selected network.

3. **Create or import an account.** Wallet → **New NEAR account**. The key is
   shown once; save it. The account ID is the 64-hex implicit account. To use
   a named testnet account instead, create it with `near-cli` or a testnet
   wallet and import its `ed25519:` key on the same page.

4. **Fund it.** Open <https://near-faucet.io>, enter the 64-hex account ID,
   and request testnet NEAR. With `near-cli`, the faucet command is
   `near account create-account fund-myself <account> '1 NEAR' autogenerate`
   for a named account; an implicit account is activated by the first
   transfer to it. Wait for the balance to appear on the Wallet page.

5. **Send a transfer.** Wallet → **Send NEAR** → recipient (for example
   `bob.testnet`, or another account you control) → amount `0.1` → **Review
   and send**. The approval window shows the receiver, the amount in NEAR and
   yoctoNEAR, the signing key and its on-chain limit. **Approve and sign**;
   the transaction hash appears with **View transaction**, which opens the
   testnet explorer.

6. **See a rejection stay unsigned.** Repeat the transfer and press
   **Reject** (or close the window). The form reports the rejection, no
   transaction is broadcast, and the balance does not change.

7. **Open a DApp.** Serve the test page:

   ```sh
   python3 -m http.server 8080 --directory web/dapp
   ```

   In the app: DApp → `http://localhost:8080` → **Open**. Press **Connect
   wallet** and approve the shell's connect dialog. Then send `0.01` NEAR to a
   testnet account and approve the transaction. The page sees only the hash;
   it never sees a key, and it cannot approve anything itself.

8. **See a function-call key limit refuse a request.** The limit is enforced
   by `neartx.Prepare` before the approval window and covered by
   `TestPrepareAppliesFunctionCallKeyLimits` and
   `TestSignNearTransactionAppliesTheKeyLimit`. To see it live, add a
   function-call key to a testnet account with `near-cli` (contract, method
   list, allowance), import that key as the wallet's signing key, and try a
   transfer or an unauthorized method. The shell refuses with a visible
   error; the approval window never opens.

9. **Ask for a NEAR Intents dry quote.** Intents → **Load supported assets**
   → pay with `wNEAR · NEAR`, receive `USDC · Ethereum`, amount `1`, a
   recipient address on Ethereum, and your NEAR account as the refund address
   → **Get a dry quote**. The approval window shows **You pay 1 wNEAR**,
   **You receive at least … USDC**, the deadline/deposit fields (a dry quote
   issues neither), the refund and recipient, and the intent sentence.
   Accept or reject; either way nothing is signed and no funds move.

10. **Ask a plugin for a NEAR signature (optional).** Install a plugin whose
    manifest grants `sign` and a NEAR chain; its `host.requestNearSign` opens
    the same approval window. The test fixtures in
    `internal/plugin/testdata/examples` show the host API. No NEAR plugin is
    shipped.

## Tests

```sh
go test ./...          # unit tests, no network
go test -race ./...    # the approval and DApp paths
# also hit the public endpoints (only these packages define -live):
go test ./internal/app/ ./internal/chains/ ./internal/chain/evm/ ./internal/chain/solana/ -live
```

The four checks the slice is judged on:

| Check | Where |
|---|---|
| A key is not written to disk | `TestNearWalletCreate` — `settings.json` holds the account, not the `ed25519:` key |
| A rejection never signs or broadcasts | `TestSignNearTransactionNeedsApproval`, `TestSendNearRejected`, `TestDappSignRejected` |
| An over-limit or unauthorized function call is refused | `TestPrepareAppliesFunctionCallKeyLimits`, `TestSignNearTransactionAppliesTheKeyLimit` |
| A WebView cannot call the approval directly | `TestDappSignNeedsAConnection`, `TestDappSignRejected`, `TestDappBridgeScript` — the page only asks; the shell's window is the only path |

Other useful tests: `TestDappOrigin` (https / loopback only),
`TestDappConnectRejected`, `TestDappAddFunctionCallKey`,
`TestQuoteAsksForADryQuote` (the request body carries `dry: true`),
`TestIntentsQuoteShowsResultLanguage`, and
`TestIntentsApprovalRejectButton`.

## Dependencies

- **Go 1.27.3**, macOS for the native UI and the system check.
- [MyGo](https://github.com/egoist/mygo) — window, native UI, keychain
  (`Secrets`), custom protocol, web view.
- [moejs](https://github.com/MoeForge/moejs) — the sandboxed JavaScript
  runtime, one realm per plugin.
- [go-ethereum](https://github.com/ethereum/go-ethereum) — EVM encoding,
  signing and RPC types.
- [`github.com/defuse-protocol/one-click-sdk-go`](https://github.com/defuse-protocol/one-click-sdk-go)
  — the official 1Click Go SDK (no transitive dependencies).
- `golang.org/x/crypto` (ed25519/base58 helpers), `purego` (LocalAuthentication
  without cgo), `golang.org/x/image` (the app icon).
- No JavaScript build step: the DApp and passkey pages are static files.

## Known limitations

- **No mnemonic / HD wallet.** NEAR keys are ed25519 key pairs; EVM keys are
  raw hex. A mnemonic is not an acceptance item for this slice.
- **Solana is read-only.** EVM and Solana signing are not part of this slice;
  only NEAR signs.
- **Named NEAR accounts are imported, not created.** The app creates implicit
  accounts; a named account's key must be imported.
- **The DApp bridge is a small custom `window.nearWallet`**, not the full
  Wallet Selector package. A page must use the injected object.
- **NEAR Intents has no testnet.** This build only asks for `dry: true`
  quotes. No deposit address is issued and no funds move. A wet quote and a
  solver execution are deliberately not implemented; they would need the
  user's explicit consent, an independent funded account, and a tiny amount.
- **Passkey is not wired.** The Touch ID prompt is a system check, not a
  WebAuthn credential. The ceremony page and the account/smart-account code
  exist, but need a relying-party domain, a bundler and the account UI.
  Recovery depends on whether the passkey is synced and on having a second
  key; a device-bound passkey can be lost, and a passkey cannot be exported.
- **macOS-first.** The system check is macOS-only; the app builds elsewhere
  but the presence check is unavailable.
- **No hardware wallet, menu bar panel, fiat prices or agent API.** Those are
  later milestones.

## Security model

- Keys live in the OS keychain, never in `settings.json`. The settings file
  keeps addresses and labels only.
- A plugin never sees a key. `host.accounts()` returns addresses;
  `host.requestSign` / `host.requestNearSign` only ask the shell, and the
  shell's approval is the only path to a signature.
- A DApp page is untrusted: no Go methods, no keychain, no filesystem, no
  permissions, no `window.open`, no downloads, and no way to draw or dismiss
  an approval. Its origin comes from the web view's URL.
- The approval window is the shell's own; a plugin or page can describe what
  it wants, but the decode shown to the user is the shell's.
- NEAR function-call key limits are checked before the approval window, so an
  over-limit or unauthorized request fails visibly instead of asking the user
  to approve something the chain will reject.
- RPC goes through the shell's pinned broker; a node that reports a hash other
  than the signed transaction's is refused.
- No auto-approval, no silent signing, and no loosening of the plugin sandbox.

## Building

```sh
go run .               # run from source
go test ./...          # unit tests
go tool mygo dev       # live reload, native UI
go tool mygo build     # package into build/
go run ./tools/icon    # regenerate resources/icon.png
```

To write an app, see [`docs/plugins.md`](docs/plugins.md); to publish and sign
one, see [`docs/store.md`](docs/store.md). To try the wallet,
open **Wallet** and choose **New account** (EVM) or **New NEAR account**; the
key is shown once, so save it.

## Milestones

| | |
|---|---|
| **M0** | Shell: desktop window with a sidebar, watch-only addresses, balances from one RPC, EVM and Solana — **done** |
| **M1** | Plugin runtime: manifest, moejs realm, limits, `setting`/`kv`/`fetch`/`render`, plugin manager, two demo plugins — **done** |
| **M2** | Plugin store: signed registry installs, permission prompts, update guards, the malicious-plugin test suite — **done** |
| **M3** | Native wallet: hot wallet in the OS keychain, decoded approvals, `host.requestSign` — **done** |
| **Hackathon** | NEAR wallet and approvals, function-call key limits, DApp web view and bridge, plugin NEAR requests, Intents dry quote — **this slice** |
| **Next** | Passkey account (WebAuthn + ERC-4337), hardware wallet, agent API, menu bar panel |

## License

[MIT](LICENSE).
