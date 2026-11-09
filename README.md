# Lintel

**A native desktop app for a multi-chain wallet and sandboxed apps. Keys stay in the OS keychain, and every signature goes through the shell's own approval.**

The window is the product: a sidebar of Portfolio, Wallet, Apps, Intents, DApp, and Settings. It is not a menu-bar widget and not a metrics-only dashboard. A wallet is a named container. Creating one prepares an EVM account, a NEAR account, and a Solana account together, and the network menu shows that wallet's account for the selected network. There is no mnemonic. Apps are sandboxed plugins (`plugin.json` + `index.js`); the shell draws native page blocks, and a plugin never supplies HTML. A read-only agent API on loopback can read the portfolio and the installed apps. It cannot sign, approve, or install.

NEAR is the first chain it can truly sign on and connect to web apps: a native
NEAR wallet whose ed25519 keys live in the OS keychain, a shell-drawn approval
for every transfer, function call and access key, an isolated web view with an
injected NEAR wallet bridge, and a dry-run NEAR Intents quote shown in the
language of the result.

Design: [`docs/design.md`](docs/design.md). License: [MIT](LICENSE).

**中文简介**：门厅（Lintel）是一个 Go + MyGo 写成的原生桌面应用，不是菜单栏小组件，也不是只看指标的仪表盘。侧边栏是持仓、钱包、应用、Intents、DApp 和设置。插件是沙箱里的
JavaScript，页面是数据，钥匙留在系统钥匙串，每一次签名都由壳自己解码并弹窗确认。
钱包是一个有名字的多链容器：新建时同时准备 EVM、NEAR 和 Solana 账户。
这次黑客松把 NEAR 接成第一条能真正签名、能连接网页应用的链：测试网 NEAR 钱包、
原生 NEAR 批准窗、受限的 function-call access key、隔离 WebView 里的
`window.nearWallet` 桥、只跑 `dry: true` 的 NEAR Intents 报价，以及一个真正的
WebAuthn Passkey 钱包：同一把 Passkey 控制一个 NEAR 主网 `0s…` 账户、一个在所有
EVM 链上地址相同的 ERC-4337 智能账户，以及一个 Solana 主网上的 LazorKit v1
金库地址。Passkey 的私钥留在认证器里。NEAR 交易由钱包自己的中继账户代付 gas；
EVM 发送和 Solana 金库的创建、发送都还没有接入。

## The hackathon slice

- **Tracks:** `security` (main narrative) and `defi` (secondary).
- **Baseline:** tag [`pre-hackathon`](https://github.com/DaviRain-Su/lintel/releases/tag/pre-hackathon)
  at `ccde05d` — the EVM + Solana read-only shell, the sandboxed plugin runtime
  and signed store, and the native EVM wallet with decoded approvals and a
  system (Touch ID) check.
- **This slice:** [`pre-hackathon...hackathon-submission`](https://github.com/DaviRain-Su/lintel/compare/pre-hackathon...hackathon-submission)
  — NEAR testnet wallet, native NEAR approvals, function-call key limits, the
  isolated DApp web view and bridge, plugin NEAR requests, and a NEAR Intents
  dry quote. The `hackathon-submission` tag is created at submission; the link
  works once the repository is public and both tags are pushed.

What the slice deliberately does **not** do: no mnemonic/HD wallet, no Solana
transaction signing, no Phoenix/Hyperliquid, no Chain Signatures, no mainnet
Intents without the user's explicit consent, no loosening of the plugin
sandbox, and no auto-approval. The Touch ID prompt is a system check, **not**
the Passkey wallet: that is a separate WebAuthn credential. Its EVM smart
account is shown and watched but not yet used to send (that needs a bundler).
Its Solana address is the LazorKit v1 vault derived from the same P-256 key;
creating that vault and sending SOL are not wired.

## What it is

- **The shell** (Go + [MyGo](https://github.com/egoist/mygo) native UI): a
  normal desktop window with a sidebar of pages — Portfolio, Wallet, Apps,
  Intents, DApp, Settings — plus the app launcher, permission prompts, decoded
  approvals and chain brokers.
- **Apps are plugins** (JavaScript, anyone's): a folder with `plugin.json` and
  one module, run each in its own [moejs](https://github.com/MoeForge/moejs)
  runtime — pure Go, one realm each, with memory and time limits — and only the
  host functions its manifest grants. Installed apps sit on the Apps page as a
  launcher: click one to open it, or open its settings, logs and switch. A
  first launch includes two official examples, Gas and Fear & Greed, so that
  launcher shows how an app is opened. Any other app installs from a folder,
  a local zip, or the store, with its permissions shown before anything is
  unpacked.
- **Plugin UI is data.** A plugin returns pages (`stats`, `table`, `cards`,
  `links`, `chart`); the shell draws them with native UI. No HTML, no
  plugin-drawn dialogs, so an approval prompt cannot be imitated.
- **DApps are untrusted pages.** The DApp page opens an https URL (or http on
  localhost) in an isolated web view with a small injected NEAR wallet bridge.
  The page can ask to connect and to sign; it cannot read a key, call a Go
  method, open a window, download a file, or draw an approval.
- **The wallet is native.** A wallet is a named container, not a single key.
  Creating one prepares an EVM account, a NEAR implicit account, and a Solana
  account together. Their keys live in the OS keychain and never reach a
  plugin or a web page. The Wallet page shows the account for the network
  that is selected. Importing a private key targets that wallet and that
  chain. Signing is only ever the shell's own decoded approval.
- **A passkey wallet.** One WebAuthn passkey, created in the system browser,
  controls a NEAR mainnet deterministic account (`0s…`, NEP-616), an ERC-4337
  smart account at the same address on every EVM chain, and a Solana vault
  derived from the same P-256 key. The NEAR account's transactions are relayed
  by the wallet's own unlocked NEAR account; the passkey signs in the browser
  and its private key never enters the app. EVM send and Solana vault creation
  are not wired.
- **A read-only local API** (opt-in): a loopback HTTP server with a bearer
  token, so a local agent can read the portfolio and the installed apps. It
  cannot sign, approve or install.

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
- **Bridge**：主 frame 的 preload script 提供 `window.nearWallet`，支持
  `connect`、`signIn`、`disconnect`、`signOut`、`getAccounts`、`getNetwork`、
  `signAndSendTransaction` 和 `addFunctionCallKey`。请求发送到 shell 的
  `cryptopanel://near/request`；不受信任的页面不能读取异源响应，因此结果通过事件返回。
- **Origin 与文档**：请求须携带原生 host 向加载完成的主文档发放的随机 capability。
  Host 共同校验当前 WebView、实际 origin 与文档 capability；顶层 URL 或 HTTP
  `Origin` header 不能单独授权 frame。仅接受 `https` 或 loopback 上的 `http`。
  每个 origin 须先得到用户连接批准，未连接的页面不能签名。导航、关闭窗口、断开连接
  或请求超时会撤销待批操作；旧文档的 capability 不能在新文档中复用。
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

A first launch also includes two official examples, so the launcher is not
empty: **Gas** (`com.example.gas`) and **Fear & Greed**
(`com.example.feargreed`). Gas names chains for `host.rpc` and never an RPC
URL; Fear & Greed uses allowlisted `host.fetch`. The launcher labels them
**Official**. They run with only the permissions in their manifests, so they
cannot see a key and cannot draw or dismiss an approval. Removing one leaves
it removed. Folder, zip and store installs are unchanged for every other app,
and a folder or zip install stays labeled **Unsigned**.

Local installs are unsigned and the app says so. See
[`docs/store.md`](docs/store.md) for the registry format and signing.

### Local agent API

Settings → **Local API** turns on a read-only HTTP API on loopback, for a
local agent to read the app. It listens on `127.0.0.1` only, requires a
bearer token (kept in the OS keychain, rotatable in Settings), and refuses
requests that carry a browser `Origin`:

```
GET  /v0/portfolio            → network, block, accounts and token balances
GET  /v0/plugins              → installed apps and their permissions
POST /v0/plugins/{id}/refresh → asks an app for its first page, as data
```

```sh
curl -H "Authorization: Bearer <token>" http://127.0.0.1:8765/v0/portfolio
```

Amounts are decimal strings in the smallest unit, with the symbol and
decimals beside them. The API cannot sign, approve, install or reach a key:
an app's `requestSign` is refused over the API before any approval window
can open. There is no prices endpoint. The window can show an approximate USD value from CoinGecko; this API does not.

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
- **Requested by** — `Lintel · dry run`.

Accepting a dry quote records the conditions and says plainly that nothing
was signed or sent. Rejecting it changes nothing. A real swap (a wet quote,
a deposit, a solver execution) is deliberately **not** implemented in this
build; it would need the user's explicit consent, a separate funded
development account, and a much smaller amount. NEAR Intents has no testnet.

### Passkey wallet

The Touch ID / Face ID prompt in the wallet is a **system presence check**
(LocalAuthentication), not a WebAuthn Passkey. The passkey wallet is a
separate credential, created on the Wallet page:

- **Create**: the app serves a small page on `http://localhost:<random port>`,
  opens it in the system browser, and the platform authenticator runs
  `navigator.credentials.create`. The app checks the attestation's challenge,
  origin and relying party (`localhost`) and stores only the credential id and
  the P-256 public key.
- **NEAR account**: the passkey's P-256 key owns a NEP-616 deterministic
  account (`0s` + the StateInit hash) running the NEP-591
  `p256-passkey-wallet-contract.trezu.near` global contract. The contract is
  **mainnet only**; the card says so on every other network.
- **Create the account / send NEAR**: the app shows its own decode first
  (recipient, amount, the passkey's public key, the relayer). After approval,
  the passkey signs the wallet contract's message in the browser, and the
  wallet's own unlocked NEAR account relays
  `[DeterministicStateInit, w_execute_signed]` and pays the fee. The relayer
  cannot change the transfer: the passkey's signature covers the inner
  message. A rejection broadcasts nothing.
- **EVM account**: the same P-256 key owns a Coinbase Smart Wallet (ERC-4337
  v0.7) at the same address on every EVM chain. The app reads the
  counterfactual address from the factory and watches it. Sending from the EVM
  account needs a bundler and is **not** wired in this build.
- **Solana vault**: the same P-256 key determines a LazorKit v1 vault PDA on
  Solana mainnet. The program is
  `LazorjRFNavitUaBu5m3WaNPjU1maipvSW2rZfAFAKi` (deployed). The user seed is
  SHA-256 of a domain label and the compressed public key; the wallet PDA is
  `["wallet", seed]` and the vault, which is the address shown, is
  `["vault", wallet]`. A passkey cannot be an ed25519 Solana address. The
  app stores the credential id and the public key and derives the vault; it
  does not submit `createWallet`, so the vault is not created and must not be
  funded yet. The program does not bind that seed to the passkey, and v1 may
  refuse new creates once its sunset binary is deployed. v2 mainnet
  (`LazorFroi…`, seeds prefixed `lk2:`) was not deployed when this was
  written. Devnet v1 is a different program id and a different vault; the
  app's Solana network is mainnet. Sending SOL is **not** wired.

Restoring the original wallet requires **both the original usable passkey and
its public recovery file**:

- **Export recovery data** on the Wallet or Settings page before removing the
  wallet or moving to another device. The JSON contains the credential id,
  P-256 public key and derived NEAR address, never the passkey's private key.
  These public identifiers can link your accounts, so keep the file private.
- **Restore passkey** imports that file, checks its format and derived address,
  then asks the original credential for a fresh WebAuthn assertion. The app
  verifies the challenge, origin, relying party, user verification and signature
  before saving the restored identity. It does not create a replacement key.
  The EVM address is read again from the smart-account factory after recovery.
- **A synced passkey** (Apple Passwords / iCloud Keychain or Google Password
  Manager) can supply the original credential on another device. Syncing that
  credential alone does not restore the app's public credential-to-account
  mapping: the recovery JSON is also required in this version.
- **A device-bound passkey** may be permanently unavailable after losing its
  device. The recovery JSON alone cannot sign or replace a lost passkey;
  conversely, this version cannot reconstruct lost public recovery metadata
  from a synced credential alone.
- NEAR supports multiple keys in general, but this passkey wallet does **not**
  yet provide an interface for enrolling another recovery key. Do not rely on
  an unconfigured second key or an automatic recovery service.
- A passkey's private key **cannot be exported** and re-imported like a mnemonic.

## Try it: step by step

Requirements: macOS (the native UI and the system check), Go 1.27.3, and
network access to the NEAR testnet RPC and 1Click.

1. **Run the app.**

   ```sh
   go run .
   ```

2. **Switch to NEAR Testnet.** Settings → Chain type → NEAR → Chain → NEAR
   Testnet. The sidebar footer shows the selected network.

3. **Create or import a wallet.** Wallet → **New wallet**. That prepares an
   EVM key, a NEAR implicit account, and a Solana key together, and shows each
   secret once; save them. There is no seed phrase. The network menu on the
   page lists Ethereum, Base, NEAR, NEAR Testnet, and Solana; choosing one
   shows that wallet's account for it and updates the sidebar. To use a named
   testnet account instead, create it with `near-cli` or a testnet wallet,
   choose NEAR Testnet, and import its `ed25519:` key into the
   open wallet.

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

10. **Create a passkey wallet (NEAR mainnet).** The wallet contract is
    mainnet-only, so this step needs a funded NEAR mainnet account as the
    relayer. Switch to NEAR mainnet, import and unlock that account, then
    Wallet → **Create passkey**. The system browser opens a local page and the
    platform authenticator creates the credential. The card shows the
    deterministic `0s…` NEAR account. Switch chain to see the EVM smart
    account and the Solana vault the same key determines. **Create account**
    deploys the NEAR account through the relayer; **Sign and send** approves a
    transfer, then asks the passkey to sign it in the browser. The approval
    window shows the inner transfer, not the relayer's envelope; the passkey's
    signature covers exactly that transfer. Solana shows the vault address and
    does not send.

11. **Ask a plugin for a NEAR signature (optional).** Install a plugin whose
    manifest grants `sign` and a NEAR chain; its `host.requestNearSign` opens
    the same approval window. Gas and Fear & Greed ship with the shell as
    official examples of how apps are used; neither one asks to sign, and no
    NEAR plugin is shipped. Their source is
    `internal/plugin/testdata/examples`.

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

- **No mnemonic / HD wallet.** A wallet is a named group of independent keys:
  NEAR and Solana are ed25519 key pairs, EVM is a raw secp256k1 hex key. A
  mnemonic is not an acceptance item for this slice.
- **Solana keys are held, not used to sign.** Creating or importing a Solana
  account puts an ed25519 key in the keychain and the Wallet page can show
  its balance. Building and sending a Solana transaction is not part of this
  slice; only NEAR signs.
- **Named NEAR accounts are imported, not created.** The app creates implicit
  accounts; a named account's key must be imported.
- **The DApp bridge is a small custom `window.nearWallet`**, not the full
  Wallet Selector package. A page must use the injected object.
- **NEAR Intents has no testnet.** This build only asks for `dry: true`
  quotes. No deposit address is issued and no funds move. A wet quote and a
  solver execution are deliberately not implemented; they would need the
  user's explicit consent, an independent funded account, and a tiny amount.
- **The passkey wallet is NEAR-mainnet-only for signing.** The NEAR wallet
  contract is deployed on mainnet; testnet has no matching contract. The EVM
  smart account address is derived and watched, but sending from it needs an
  ERC-4337 bundler and is not wired. The Solana vault address is derived from
  the LazorKit v1 program and watched, but the account is not created and
  sending is not wired; do not fund that address yet. Recovery requires the
  original usable passkey plus its exported public recovery JSON. Syncing the
  credential alone is insufficient; the private key cannot be exported, and a
  lost device-bound credential cannot be replaced by the JSON.
- **macOS-first.** The system check is macOS-only; the app builds elsewhere
  but the presence check is unavailable.
- **No hardware wallet.** That is a later milestone.

## Security model

- 私钥保存在 OS keychain，绝不写入 `settings.json`。设置文件保存地址、标签、
  应用配置和 Passkey 的公开身份资料（credential ID、公钥与派生地址），不保存私钥。
- A plugin never sees a key. `host.accounts()` returns addresses;
  `host.requestSign` / `host.requestNearSign` only ask the shell, and the
  shell's approval is the only path to a signature.
- DApp 页面不受信任：没有 Go 方法、keychain、文件系统或权限授权，不能使用
  `window.open`、下载文件或绘制、关闭原生批准窗。请求必须通过当前 WebView、
  原生读取的 origin 和当前主文档 capability 的共同校验；不能只凭顶层 URL 授权。
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
open **Wallet** and choose **New wallet**. That creates an EVM, NEAR, and
Solana account together and shows each key once, so save them. The chain
control on the page chooses which account is shown.

## Milestones

| | |
|---|---|
| **M0** | Shell: desktop window with a sidebar, watch-only addresses, balances from one RPC, EVM and Solana — **done** |
| **M1** | Plugin runtime: manifest, moejs realm, limits, `setting`/`kv`/`fetch`/`render`, plugin manager, two demo plugins — **done** |
| **M2** | Plugin store: signed registry installs, permission prompts, update guards, the malicious-plugin test suite — **done** |
| **M3** | Native wallet: hot wallet in the OS keychain, decoded approvals, `host.requestSign` — **done** |
| **Hackathon** | NEAR wallet and approvals, function-call key limits, DApp web view and bridge, plugin NEAR requests, Intents dry quote — **this slice** |
| **M4 (part)** | Read-only local agent API: loopback HTTP, bearer token in the keychain, `/v0/portfolio`, `/v0/plugins`, `POST /v0/plugins/{id}/refresh` — **done** |
| **Passkey** | WebAuthn passkey wallet: NEP-616 NEAR account, NEP-591 contract, relayed deploy and send; the same key's ERC-4337 address on EVM; a LazorKit v1 Solana vault address derived from that key (create and send not wired) — **done** |
| **Next** | EVM passkey sending through a bundler, Solana passkey vault creation and sending, hardware wallet |

## License

[MIT](LICENSE).
