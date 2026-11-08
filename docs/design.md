# Crypto Panel — design

**Status:** draft, for review. **Repo:** this one — `crypto-panel`, a project of its own,
built on [MyGo](https://github.com/egoist/mygo) (and its Zig bindings as an option).
**References:** [Metrix.Bar](https://metrix.bar),
[mygo-music](https://github.com/egoist/mygo-music).

## 1. What it is

A native desktop app for watching crypto: balances, positions, prices, gas, yields,
NFTs, and whatever a plugin shows — in one window with a sidebar of pages.
**Self-custody.** It has a native wallet whose keys live in the OS keychain;
anything that moves funds is signed by the shell after a decoded approval, and
never passes through a plugin.

The longer aim is a host for blockchain apps, the way a music app hosts sources:
each app is a plugin with its own pages, and adding an app is installing a plugin.
What those apps all need is a wallet, so the native wallet — watch-only
addresses, then accounts whose keys the shell holds — is the milestone that
opens the rest.

The app is a shell plus sandboxed plugins:

- **Shell** (trusted, Go + MyGo native UI): the main window and its sidebar,
  plugin pages, the plugin manager, permission prompts, chain brokers, approval
  UI, key custody.
- **Plugins** (untrusted, JavaScript): a folder with `plugin.json` and one module,
  run each in its own runtime, which can only reach what its manifest grants.
- **Plugin UI is declarative**: a plugin returns page data; the shell renders it
  with native UI. Plugins never draw, never inject HTML, never see keys.

## 2. What we take from each reference

### Metrix.Bar — the product shape

| Idea | How we use it |
|---|---|
| One number in the menu bar, click for a panel | The shell's home is a desktop window with a sidebar of pages, as in mygo-music; the menu bar headline is a later entry point (M4) |
| A dashboard is one file (`<name>.bar`) with permissions, inputs, script | A plugin is one folder with `plugin.json`: permissions, settings (native form), one JS module |
| Permissions are readable, requests elsewhere are refused | `permissions.network` (host allowlist), `permissions.chains`, `permissions.read`; enforced in the host, never in the plugin |
| Local API for agents, localhost + bearer token | `GET /v0/portfolio`, `/v0/plugins`, `POST /v0/refresh`; read-only, nothing that signs |
| Screen sidebars and global shortcuts as entry points | Later milestones: `mygo.GlobalShortcut` and a panel window at a screen edge |
| Free tier limited by enabled dashboards | Not a product decision here; a plugin registry can count them |

Not taken: their Worker sandbox. We run plugins in Go's JS engine (below), so the
sandbox is the plugin runtime's, not a browser's.

### mygo-music — the plugin model

Concretely, from `internal/plugin`:

| Idea | Source | How we use it |
|---|---|---|
| A plugin is a folder: `plugin.json` + one module | `internal/plugin/plugin.go` | Same layout, same install flows (GitHub, URL, zip, folder) |
| Each plugin gets its own runtime | `moejs`, one realm per plugin | Same: `moejs` is pure Go, passes test262, and runs here without cgo |
| Memory and time limits, restart on abuse | 256 MB, 30 s calls, 1 s stuck, restart | Same limits at first; tune with real plugins |
| Capabilities are opt-in and host-enforced | `fetch` only to `permissions.network` | Same, plus chain and read scopes; **no `process`** |
| Plugins return page data, the app draws it | `home()`, `browse({page})` | Same shape (`home`, `page({name})`), rendered by `ui` |
| Settings become a native form; secrets go to the keychain | `host.setting`, `host.webSignIn` | Same for exchange keys, read-only |
| Install from GitHub/zip, remove from the app | `internal/plugin/install.go` | Same, plus signature and hash checks (crypto needs them) |

What changes for crypto: **no `host.spawn`**, **no keys in the sandbox**, and a
`host.requestSign` that is a *request* the user approves in host UI.

### MyGo / MyZig — the framework

| Need | MyGo |
|---|---|
| Main window, sidebar, pages | `mygo.NewWindow`, `package ui`, `examples/menubar-flyout` for a tray later |
| Native UI pages | `package ui` (and this fork's Zig SDK, 297 Element methods) |
| HTTP and WebSocket | `plugins/fetch`, `plugins/websocket` (Go), `zig/host/fetch`, `zig/host/websocket` |
| Secrets | `mygo.Secrets` (Keychain, libsecret, CredMan) |
| Custom scheme, local API | `mygo.Protocol`, `mygo.FileServer` |
| Global shortcuts | `mygo.GlobalShortcut` |
| Plugins for pages | `mygo.Use` — for *trusted* Go plugins only; runtime plugins are ours |
| Testing | `ui.Tester`; automation plugin for end-to-end |

If the shell is written in Zig, this fork's `zig/host/*` covers all of the above;
the plugin runtime stays in Go (moejs), behind an opt-in host package (§10).

## 3. Architecture

```
┌──────────────────────────────────────────────────────────────┐
│ Shell — trusted, Go + MyGo native UI (or Zig via zig/host)    │
│   window · sidebar · plugin manager · permission prompts      │
│   approval UI (decoded transactions) · settings               │
├──────────────────────────────────────────────────────────────┤
│ Chain layer — trusted                                         │
│   RPC broker (allowlist, cache, rate limits, audit log)       │
│   price feeds · token lists · decoders · hardware wallet bridge│
├──────────────────────────────────────────────────────────────┤
│ Plugin host — untrusted code, host-enforced capabilities      │
│   moejs runtime per plugin · limits · manifest · permissions   │
├──────────────────────────────────────────────────────────────┤
│ Plugins — anyone's JavaScript                                 │
│   home() · page({name}) · refresh() · fetch (allowlisted)     │
└──────────────────────────────────────────────────────────────┘
```

Everything a plugin can do is a host function in §6. If it is not in that table, it
does not exist for plugins.

### Chain families

The chain layer speaks two families, each with its own address format, RPC
dialect and token standard:

| Family | Networks | Address | Pin | Tokens |
|---|---|---|---|---|
| EVM (`eip155:*`) | Ethereum, Base | `0x` + 40 hex, EIP-55 | `eth_chainId` | ERC-20 `balanceOf` |
| Solana (`solana:*`) | mainnet | base58, 32 bytes | `getGenesisHash` | SPL and Token-2022 accounts |

A network is identified by its CAIP-2 id (`eip155:8453`,
`solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp`), which is what settings store and
what plugins name in `permissions.chains`. Both families sit behind one
`chain.Reader` interface (`Account`, `Block`, `RPC`), so the shell — and later
the plugin broker — does not branch on family. Adding a third family means a
new reader package and a preset, not a change to the shell.

## 4. Plugin package

```
my-portfolio/
  plugin.json
  index.js
  icon.svg          optional
  locales/          optional, en.json, zh-Hans.json …
```

```json
{
  "id": "com.example.portfolio",
  "name": "Portfolio",
  "version": "1.2.0",
  "minAppVersion": "0.1.0",
  "main": "index.js",
  "permissions": {
    "network": ["api.coingecko.com", "eth.llamarpc.com"],
    "chains": ["eip155:1", "eip155:8453"],
    "read": ["balances", "prices", "tokens", "nfts"],
    "sign": false,
    "storageKb": 256
  },
  "settings": [
    { "key": "apiKey", "label": "API key", "secret": true, "help": "From the provider's page",
      "link": "https://example.com/keys" }
  ],
  "navigation": [
    { "title": "Portfolio", "page": "overview", "icon": "chart" },
    { "title": "History", "page": "history", "icon": "clock" }
  ]
}
```

Rules:

- `id` is reverse-DNS and immutable; `version` is semver; `minAppVersion` gates the
  app's API.
- Signing is not in the manifest: a manifest cannot sign itself. A network install
  comes from a registry entry that carries the hash of the unpacked tree, an
  ed25519 signature over it, and the publisher's public key; the shell records
  them in `install.json` and checks the tree again at load. A plugin installed
  from a local folder is unsigned, and the shell says so in every page it draws
  (a badge, not a modal).
- Unknown `permissions` keys are an install error, never ignored.
- `sign: true` asks the user for the signing capability at install time and again
  per transaction.

### The store

A registry is one static JSON file: `{version: 1, name, plugins: [...]}`, each
entry naming the plugin's id, name, version, archive URL, `sha256:` hash,
`ed25519:` signature and publisher key. The app ships no catalog: with no
registry set, the Store tab says so and points at Settings. An install
downloads the archive, unpacks it to a temporary folder, checks the manifest's
id and version against the entry, checks the tree's digest, verifies the
signature, and only then shows the permission prompt. Updates must be newer,
must not replay a version with other contents, and must be signed by the same
publisher key; settings and `kv.json` carry over. The signing tool is
`tools/pluginsign`; `docs/store.md` is the publisher's guide.

## 5. Plugin API

Module exports, one function per capability; what is not exported is not done.
Every function takes one object and answers a value or a promise.

```js
// The panel's home: sections of cards, lists, tables, charts.
export function home({ chain, address }) { … }

// A page of the plugin's own, from `navigation`.
export function page({ name }) { … }

// Data the shell asks for on a schedule or on demand.
export function refresh() { … }

// Settings changed by the user.
export function settingsChanged({ key, value }) { … }
```

Errors are values with a code, as in mygo-music: `signIn`, `setup`, `unavailable`,
`unsupported`, so the shell can show the right affordance instead of a raw message.

M1 implements `page({name})`: the manager opens the pages a plugin's `navigation`
declares, and a plugin without navigation has nothing to open yet. `home`, `refresh`
and `settingsChanged` arrive with the pages that use them (the portfolio headline,
the refresh loop, the settings form's save).

## 6. Host functions

| Function | Permission | Notes |
|---|---|---|
| `host.setting(key)` | — | A setting from the manifest's form; secrets are returned only to the plugin that owns them |
| `host.kv.get/put/delete` | `storageKb` | Per-plugin namespace, quota enforced |
| `host.fetch(url, init)` | `network` | Host makes the request; origin must match the allowlist (`*.example.com` allowed); 30 s timeout, 8 MB cap, no arbitrary headers beyond a safe set |
| `host.rpc(chain, method, params)` | `chains` | Goes through the RPC broker; the plugin never picks a URL |
| `host.accounts()` | `read: balances` | Addresses only. Never keys, never signatures |
| `host.prices(ids)` | `read: prices` | The shell's price feed |
| `host.render(spec)` | — | Validates a page tree (below) and returns a handle |
| `host.requestSign(tx)` | `sign` | Queues an approval; resolves with a tx hash or a rejection. Never called without the user's click |
| `host.log(level, msg)` | — | Plugin log panel |
| `host.i18n(key, args)` | — | The app's language |

Deliberately absent: `host.spawn` (mygo-music's process permission), raw sockets,
clipboard reads, filesystem, notifications, and any way to draw outside `host.render`.

What M1 installs: `setting`, `kv`, `fetch`, `rpc`, `accounts`, `log`, `i18n`,
`render`. M3 adds `requestSign`, behind `permissions.sign` (which needs at least
one chain). `host.prices` waits for the shell's price feed. Hooks are synchronous
— the host functions are, so a plugin has nothing to await — and a promise a hook
returns is read only if it is already settled; `requestSign` blocks the call while
its approval window is open, and the runner's watchdog is paused for that wait.
`host.rpc` answers with the decoded JSON value (a hex quantity is a string, a
block is an object), and it refuses `eth_send*`, `eth_sign*`, `personal_*`,
`wallet_*` and any `sign*` method: a plugin reads the chain, the shell signs.

### The native wallet

The Wallet page is the shell's own: accounts are created (a fresh key) or
imported (a private key), each stored in the OS keychain under `key:<address>`,
never in `settings.json`. The settings file keeps only addresses and labels, and
the keyring reads a key into memory only while its account is unlocked. Wallet
accounts are watched like any other address, so their balances join the
portfolio; a wallet address cannot also be added as a watch-only one.

The wallet's second factor is the system's own check. On macOS,
`internal/passkey` calls LocalAuthentication through purego — no cgo, so the
build stays cross-compilable — and asks for Touch ID, Face ID, Optic ID or the
login password before an unlock, a key reveal, or a signature. The wallet locks
when the screen locks, when the window closes, and after five minutes idle.
This is deliberately the platform-authentication half of a passkey, not a
WebAuthn credential: a passkey's P-256 key cannot sign a secp256k1 transaction
without a smart account, a relying party and an associated domain, so the
signing key stays in the keychain and the system check guards every use of it.
The switch is per user (`disablePresence` in settings), on by default.

`host.requestSign(tx)` takes `{chainId, from?, to, value?, data?, gas?,
maxFeePerGas?, maxPriorityFeePerGas?, nonce?, description?}`. The shell picks the
account (the named one, or the first unlocked), fills in the nonce, gas estimate
and fees, and decodes the calldata itself. The approval window shows that decode,
the network, the value and the most the fee can cost, with the plugin's
`description` beside it and a note that the plugin's words are not verified. Only
**Approve and sign** reaches the key; **Reject**, closing the window, or a locked
account ends the request with an error the plugin sees. The signed transaction is
broadcast through the pinned broker, and the plugin gets its hash. A node that
reports a hash other than the signed transaction's is refused.

### Passkey accounts (in progress)

A passkey cannot sign a secp256k1 transaction. The account that owns a
passkey is therefore an ERC-4337 smart account that verifies P-256 on chain.
`internal/webauthn` parses and verifies the two ceremonies (an
attestationObject into a P-256 public key, an assertion into
authenticatorData/clientDataJSON/signature) and checks the challenge, origin
and relying party. `internal/smartaccount` builds the counterfactual address,
the v0.7 user operation hash, the `WebAuthnAuth` signature the account
verifies, and the bundler calls. The browser ceremony page, the app's account
UI and the testnet integration are the next steps; they need a relying-party
domain and a bundler.

## 7. UI model

A page is data. The shell renders it with native UI, in the app's theme, so a plugin
cannot imitate an approval prompt or a system dialog.

The look is graphite, paper and a sage accent: warm off-white paper in light,
near-black graphite in dark, one sage green for actions and status. Cards are
quiet — a hairline border, a small shadow, generous spacing — and the app's own
chrome (the sidebar, page headers, the approval window) is never drawn by a
plugin.

```js
export function page({ name }) {
  return {
    title: "Portfolio",
    subtitle: "Base and Ethereum",
    sections: [
      { title: "Total", stats: [{ label: "Value", value: "$12,480.20" }] },
      { title: "Tokens", table: {
          columns: [{ title: "Token" }, { title: "Balance", align: "end" }],
          rows: [["ETH", "3.204"], ["USDC", "1,200.00"]],
      } },
      { title: "Pools", cards: [{ title: "ETH/USDC", subtitle: "0.05%", body: "APR 12.4%" }] },
      { title: "More", links: [{ title: "History", page: "history" }] },
    ],
  };
}
```

Supported blocks at M1: `stats`, `table`, `cards`, `links`, `text`, `divider`,
`chart` (line/bar, data only). Every string is plain text — no HTML, no markdown
beyond inline emphasis, no images from the network except through `host.fetch`
(which needs the allowlist).

## 8. Security

Assets to protect, in order: keys, approvals (what the user signs), addresses and
balances (privacy), the machine.

| Threat | Mitigation |
|---|---|
| A plugin exfiltrates addresses/balances | Network allowlist enforced in the host; `host.accounts` returns only what the page asks for; the shell logs every origin each plugin reached |
| A plugin phishes with a fake approval | Approvals are host-rendered, never plugin-drawn; `host.render` has no `dialog`/`confirm` block at all |
| A plugin asks to sign something else than shown | Shell decodes the transaction itself (to, value, calldata, token approvals) and shows its own decode next to the plugin's description; a mismatch is highlighted |
| A plugin drains CPU/memory | One runtime per plugin, 256 MB cap, 30 s call timeout, 1 s stuck detection, restart; the manager shows a plugin that was restarted |
| Malware on the machine uses the wallet key | The key lives in the OS keychain; unlock, reveal and every signature ask for the system check; the wallet locks on screen lock, window close and five minutes idle |
| Supply chain: a plugin update is malicious | The registry entry's `hash` and `signature` are verified at install; the digest is checked again at load; an update must be newer, must not replay a version with other contents, and must keep the publisher's key; the permission prompt lists new permissions |
| RPC endpoint lies (MITM, wrong chain id) | The broker pins chain id per endpoint and refuses mismatches; response decoding for balances uses typed ABI decoding, and a disagreement between two endpoints can be surfaced |
| Malicious `minAppVersion` / manifest tricks | Manifest schema is strict: unknown keys fail the install |

Key custody:

- **M0–M2: watch-only.** Addresses are added, never keys.
- **M3 (done): hot wallet** in the OS keychain. Accounts are opt-in: the Wallet
  page starts empty, and a key enters only through Create or Import. The shell
  never exposes a key to a plugin; a signature exists only after the user
  approves the shell's own decode and, when enabled, the system check.
- **M4: hardware wallets.** The only way to sign in the recommended setup.

## 9. Menu bar (later) and agent API (Metrix.Bar)

- **The app is a window first.** The portfolio's headline lives in the window's
  sidebar and page header. A menu bar item showing the headline number, opening
  the window on click, is an M4 entry point; the plugin that provides the
  headline is a setting (`navigation` + `headline: true` in the manifest).
- **Agent API**: `localhost` HTTP with a bearer token the user can rotate:

  ```
  GET  /v0/portfolio            → totals, tokens, chains
  GET  /v0/plugins              → installed plugins and their permissions
  POST /v0/plugins/{id}/refresh → asks a plugin for new data
  GET  /v0/prices?ids=eth,usdc
  ```

  Read-only by construction: no endpoint signs, and `requestSign` is not reachable
  over the API. An agent can prepare a transaction *description* to show the user,
  not approve one.

## 10. Changes this repository would carry

The app is its own repo, but two things belong here:

1. **`zig/host/plugin` (opt-in)** — if the shell is Zig: exposes the plugin manager to
   the SDK (`Plugin.list/install/enable/disable`, permission prompts as host-rendered
   pages) and hands declarative page JSON to Zig, which renders it with `ui`. Go keeps
   moejs, the broker and the crypto. Without importing the package, nothing changes
   for other apps.
2. **A declarative page renderer** — either in the app (first) or as `ui/pages` in this
   repo later, if other apps want the same "plugin data → native UI" model.

Nothing in `package ui` needs to change for M0–M2.

## 11. Repository layout

```
crypto-panel/
  main.go                 mygo.App.SetName; app.New; App.Run
  internal/app/           shell: window, sidebar, routes, pages, settings
  internal/plugin/        manifest, moejs runtime, host API, store, registry, install
  internal/plugin/testdata/examples/  example plugins used by the tests
  internal/pages/         declarative page tree → ui
  internal/passkey/       the system check: LocalAuthentication via purego
  internal/webauthn/      WebAuthn registration and assertion verification
  internal/smartaccount/  ERC-4337 user operations, WebAuthn signatures, bundler
  internal/chain/         chain vocabulary: Family, Network, Token, Reader, formatting
  internal/chain/rpc/     shared JSON-RPC broker: cache, spacing, size cap, audit
  internal/chain/evm/     eip155 reader: chain-id pin, ERC-20 balances
  internal/chain/solana/  Solana reader: genesis pin, lamports, SPL token accounts
  internal/chains/        network presets (Ethereum, Base, Solana) and reader factory
  internal/wallet/        addresses, and the keyring: accounts + keys in the OS keychain
  internal/evmtx/         EVM transactions: nonce, gas, fees, ERC-20 decode, sign, send
  internal/agent/         local API
  internal/i18n/          en, zh-Hans
  resources/icon.png      the app icon, generated by tools/icon
  tools/icon/             draws the icon with x/image/vector
  tools/pluginsign/       packs and signs a plugin for a registry
```

## 12. Milestones

| | Scope | Done when |
|---|---|---|
| **M0** | Shell: desktop window with a sidebar, native UI, settings, i18n; watch-only addresses; balances from one RPC on EVM (Ethereum, Base) and Solana; no plugins | A user opens the app from the Dock, adds an address and sees balances in a native window on macOS and Windows |
| **M1** | Plugin runtime: manifest, moejs realm, limits, `setting`/`kv`/`fetch`/`rpc`/`render`, manager UI (install from folder, enable, disable, logs), two example fixtures — **done** | A third-party plugin added by hand shows a page; a plugin that fetches outside its allowlist is refused and the refusal is visible |
| **M2** | Install from GitHub/zip, signature + hash, permission prompts, restart-on-abuse, the malicious-plugin suite — **done** | All §13 fixtures behave as specified; an unsigned network install is refused |
| **M3** | Native wallet: decoded approval UI; hot wallet in the OS keychain; the system check on unlock, reveal and sign; `host.requestSign` behind `sign: true` — **done** | A signing plugin cannot get a signature without an explicit, decoded approval and, when enabled, the system check |
| **M4** | Hardware wallet; agent API; menu bar headline, screen-edge panel and global shortcut | An agent reads the portfolio over the API; a Ledger signs a transfer the user approved |

## 13. Testing

- **Plugin fixtures**, each asserting a refusal: network escape (`evil.example` in
  `fetch`), memory bomb, infinite loop, huge page, HTML in a string,
  `requestSign` without the permission, manifest with unknown keys, replayed
  version with a different hash, an unsigned network install, a tampered
  archive, a zip that escapes its folder, a symlink in an archive, and a
  publisher key that changed between versions.
- **Fake chain**: a broker that serves recorded fixtures, so tests never touch the
  network; a second endpoint that disagrees to test the chain-id pin.
- **Page renderer**: golden tests of page JSON → `ui` tree, as `ui.Tester` sees it.
- **End to end**: `mygo-automation` drives the app — install a plugin from a folder,
  grant network, see the page, deny a signing request — so the approval path is
  tested without a human.

## 14. Open questions

1. ~~**Chains first**: EVM only at M0, or Solana too?~~ Resolved: both families at
   M0 — EVM (Ethereum, Base) and Solana — behind one `chain.Reader`.
2. **RPC**: user-supplied endpoints, bundled defaults, or both with a "trusted" label?
3. **Price feed**: the app's own source (a public API) or plugin-provided? (Suggest:
   the shell provides one, plugins may add others through `host.fetch`.)
4. ~~**Plugin registry**: a static JSON on a site, a GitHub topic, or a curated list in
   the app?~~ Resolved: a static JSON registry set in Settings; the app ships no
   catalog. The registry vouches for each publisher's key; an installed plugin
   remembers the key it was installed with and refuses a different one on update.
5. **Hot wallet at all?** Watch-only + hardware covers most users; a hot wallet adds
   a class of risk the plugin sandbox cannot mitigate.
6. **Name.** `Crypto Panel`, `Ledger View`, something else; it should not collide with
   existing wallets.

## 15. PR checklist

- [ ] `plugin.json` schema + strict parsing, with tests
- [ ] moejs runtime per plugin with limits and restart
- [ ] host functions per §6, each with a permission test
- [ ] declarative page tree + native renderer + golden tests
- [ ] plugin manager UI: install, enable/disable, permissions, logs, remove
- [x] signature + hash verification at install and load
- [ ] chain broker with chain-id pin and fixtures
- [ ] window + sidebar, with the portfolio page and plugin pages
- [ ] docs: `docs/plugins.md` for this app (manifest, host functions, page blocks)
- [ ] `zig/host/plugin` only if the shell goes Zig (deferred)
