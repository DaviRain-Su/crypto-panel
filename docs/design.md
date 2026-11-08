# Crypto Panel — design

**Status:** draft, for review. **Repo:** this one — `crypto-panel`, a project of its own,
built on [MyGo](https://github.com/egoist/mygo) (and its Zig bindings as an option).
**References:** [Metrix.Bar](https://metrix.bar),
[mygo-music](https://github.com/egoist/mygo-music).

## 1. What it is

A native desktop app for watching crypto: balances, positions, prices, gas, yields,
NFTs, and whatever a plugin shows — in a menu bar panel, a window, or a page of its
own. **Watch-only by default.** Anything that moves funds is a later milestone and
never passes through a plugin.

The app is a shell plus sandboxed plugins:

- **Shell** (trusted, Go + MyGo native UI): menu bar/tray, panel, plugin manager,
  permission prompts, chain brokers, approval UI, key custody.
- **Plugins** (untrusted, JavaScript): a folder with `plugin.json` and one module,
  run each in its own runtime, which can only reach what its manifest grants.
- **Plugin UI is declarative**: a plugin returns page data; the shell renders it
  with native UI. Plugins never draw, never inject HTML, never see keys.

## 2. What we take from each reference

### Metrix.Bar — the product shape

| Idea | How we use it |
|---|---|
| One number in the menu bar, click for a panel | The shell's home: a tray item with the portfolio's headline number and a flyout panel of pages |
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
| Tray, menu bar, flyout panel | `mygo.NewTray`, `mygo.NewFlyout`, `examples/menubar-flyout` |
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
│   tray/panel · pages · plugin manager · permission prompts    │
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
  "signature": "ed25519:…",
  "hash": "sha256:…",
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
- `signature` and `hash` are required for anything installed from the network.
  A plugin installed from a local folder may be unsigned, and the shell says so in
  every page it draws (a badge, not a modal).
- Unknown `permissions` keys are an install error, never ignored.
- `sign: true` asks the user for the signing capability at install time and again
  per transaction.

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

## 7. UI model

A page is data. The shell renders it with native UI, in the app's theme, so a plugin
cannot imitate an approval prompt or a system dialog.

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
| Supply chain: a plugin update is malicious | `hash` and `signature` in the manifest, verified at install and at load; updates require a new user confirmation listing new permissions |
| RPC endpoint lies (MITM, wrong chain id) | The broker pins chain id per endpoint and refuses mismatches; response decoding for balances uses typed ABI decoding, and a disagreement between two endpoints can be surfaced |
| Malicious `minAppVersion` / manifest tricks | Manifest schema is strict: unknown keys fail the install |

Key custody:

- **M0–M2: watch-only.** Addresses are added, never keys.
- **M3: optional hot wallet** in the OS keystore. Off by default; a setting with a
  warning; the shell never exposes it to plugins.
- **M4: hardware wallets.** The only way to sign in the recommended setup.

## 9. Menu bar and agent API (Metrix.Bar)

- **Tray**: the portfolio's headline (e.g. total value or 24h change), click opens
  the panel. The plugin that provides the headline is a setting (`navigation` +
  `headline: true` in the manifest).
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
  main.go                 mygo.Use(...); App.Run
  internal/app/           shell: tray, panel, routes, pages, settings
  internal/plugin/        manifest, moejs runtime, host API, install, signature
  internal/pages/         declarative page tree → ui
  internal/chain/         RPC broker, decoders, prices, tokens
  internal/wallet/        watch-only addresses; later keystore + hardware
  internal/agent/         local API
  internal/i18n/          en, zh-Hans
  examples/plugins/       demo plugins: portfolio, gas, nft, fear-greed
```

## 12. Milestones

| | Scope | Done when |
|---|---|---|
| **M0** | Shell: tray + panel window, native UI, settings, i18n; watch-only addresses; balances from one EVM RPC; no plugins | A user adds an address and sees balances in a native panel on macOS and Windows |
| **M1** | Plugin runtime: manifest, moejs realm, limits, `setting`/`kv`/`fetch`/`render`, manager UI (install from folder, enable, disable, logs), two demo plugins | A third-party plugin added by hand shows a page; a plugin that fetches outside its allowlist is refused and the refusal is visible |
| **M2** | Install from GitHub/zip, signature + hash, permission prompts, restart-on-abuse, the malicious-plugin suite | All §13 fixtures behave as specified; an unsigned network install is refused |
| **M3** | Decoded approval UI; optional hot wallet (keystore); `host.requestSign` behind `sign: true` | A signing plugin cannot get a signature without an explicit, decoded approval |
| **M4** | Hardware wallet; agent API; screen-edge panel and global shortcut | An agent reads the portfolio over the API; a Ledger signs a transfer the user approved |

## 13. Testing

- **Plugin fixtures** (in `internal/plugin/testdata`), each asserting a refusal:
  network escape (`evil.example` in `fetch`), memory bomb, infinite loop, huge page
  tree, HTML in a string, `requestSign` without the permission, manifest with unknown
  keys, replayed version with a different hash.
- **Fake chain**: a broker that serves recorded fixtures, so tests never touch the
  network; a second endpoint that disagrees to test the chain-id pin.
- **Page renderer**: golden tests of page JSON → `ui` tree, as `ui.Tester` sees it.
- **End to end**: `mygo-automation` drives the app — install a plugin from a folder,
  grant network, see the page, deny a signing request — so the approval path is
  tested without a human.

## 14. Open questions

1. **Chains first**: EVM only at M0, or Solana too? (Suggest: EVM only, one chain.)
2. **RPC**: user-supplied endpoints, bundled defaults, or both with a "trusted" label?
3. **Price feed**: the app's own source (a public API) or plugin-provided? (Suggest:
   the shell provides one, plugins may add others through `host.fetch`.)
4. **Plugin registry**: a static JSON on a site, a GitHub topic, or a curated list in
   the app? Verifying signatures needs a key distribution story.
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
- [ ] signature + hash verification at install and load
- [ ] chain broker with chain-id pin and fixtures
- [ ] tray + panel with the headline number
- [ ] docs: `docs/plugins.md` for this app (manifest, host functions, page blocks)
- [ ] `zig/host/plugin` only if the shell goes Zig (deferred)
