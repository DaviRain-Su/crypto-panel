# Writing an app (plugin)

An app is a folder with two required files:

```
my-app/
  plugin.json   the manifest: what the app is and what it may do
  index.js      the module: one ES module exporting page()
```

The shell runs the module in its own [moejs](https://github.com/MoeForge/moejs)
runtime — a pure-Go JavaScript engine, one realm per app — and draws the pages
it returns with native UI. An app never draws, never injects HTML, never sees
a key, and cannot open a window or an approval prompt. Everything it can do is
a function the shell installs on the `host` global, granted by the manifest.

Two official examples ship with the shell, so a first launch already has
apps to open from the Apps launcher. **Gas** (`com.example.gas`) reads
Ethereum and Base through `host.rpc`, naming chains and never an RPC URL.
**Fear & Greed** (`com.example.feargreed`) fetches one allowlisted API and
draws a chart. Their source is
[`internal/plugin/testdata/examples`](../internal/plugin/testdata/examples).
They are labeled **Official**. They still run with only the permissions in
their manifests: they cannot see a key, and they cannot draw or dismiss an
approval. Installing any other app from a folder or a zip is still marked
**unsigned**.

## 个人应用与编码 agent

Apps → **创建应用** 可以让本机 ACP 编码 agent 在独立目录生成代码，校验通过后进入待确认列表。外部 agent 也可以用 [Lintel Skill](../.claude/skills/lintel-apps/SKILL.md) 和本机 HTTP API 完成同一流程；实际接口、设置和更新操作见[个人应用指南](personal-apps.md)。

个人应用使用 `local.<owner>.<slug>`，安装后标为 **Unsigned**。目录包和 `.cryptoapp.json` 单文件包使用相同校验与冻结候选；单文件包包含 `plugin` manifest 和 `main` 源码，manifest 的入口必须是 `index.js`。静态校验只解析脚本，安装提议不会执行或注册应用。用户在原生窗口确认后才安装。

修改个人应用必须保持 id、严格提高版本，并提供当前 `packageHash` 作为 `expectedHash`。更新保留设置、secret、KV 和开关；不兼容设置迁移或 KV 缩额会拒绝。secret 的管理接口只返回是否已配置；空串写入表示清除，未提交的字段保持原值。

工具许可、安装批准与每次钱包交易批准彼此独立。Local API 没有批准、签名或广播端点，API 刷新中的 EVM / NEAR 签名请求也会被拒绝。已有文件夹、zip 和签名商店入口继续保留。真实 agent 与完整桌面流程的验证状态见[验收记录](v1-acceptance.md)。

## plugin.json

```json
{
  "id": "com.example.gas",
  "name": "Gas",
  "version": "1.0.0",
  "main": "index.js",
  "description": "Gas price and latest block on Ethereum and Base.",
  "author": "Example",
  "permissions": {
    "network": ["api.example.com", "*.example.org"],
    "chains": ["eip155:1", "eip155:8453"],
    "read": ["balances"],
    "sign": false,
    "storageKb": 64
  },
  "settings": [
    { "key": "apiKey", "label": "API key", "secret": true, "help": "Optional." }
  ],
  "navigation": [{ "title": "Gas", "page": "overview" }]
}
```

The schema is strict: an unknown key fails the install, so a typo cannot
silently widen or narrow what an app may do.

| Field | Rules |
|---|---|
| `id` | Required. Reverse-DNS, lowercase: `com.example.gas`. |
| `name` | Required, 1–64 characters. Shown on the app tile. |
| `version` | Required, semver `x.y.z` (a prerelease suffix is allowed). |
| `main` | Required. A relative `.js` or `.mjs` path inside the folder. |
| `description` | Optional, up to 400 characters. |
| `author` | Optional. |
| `minAppVersion` | Optional. The app refuses to install a plugin that needs a newer shell. |
| `permissions` | Optional, but nothing is granted by default. |
| `settings` | Up to 20 fields; each has `key`, `label`, and optional `secret`, `help`, `link` (https). |
| `navigation` | Up to 8 pages; each has `title` (1–40 characters), `page` (lowercase, digits, dashes) and an optional `icon`. The first entry is what **Open** shows. |

The menu bar reads apps too: the first stat of the first page of every
enabled app shows in the macOS tray — its value beside the icon, one menu
row per app that opens the app. A page needs no extra declaration; a page
whose first section has a `stats` block feeds the tray.

### Permissions

| Key | Grants |
|---|---|
| `network` | The `host.fetch` allowlist: exact hostnames, or `*.example.com` for every subdomain. Only `https` (or `http` on loopback) is fetched, and every redirect is checked against the same list. |
| `chains` | The CAIP-2 ids `host.rpc` and `host.requestSign` may name. The ids this shell knows are in [`internal/chains`](../internal/chains). |
| `read` | Shell data `host.accounts` may return. The only scope today is `balances`. |
| `sign` | `host.requestSign` and `host.requestNearSign`. Every request still passes the shell's own decoded approval; the app can ask, never approve. Needs at least one chain. |
| `storageKb` | The `host.kv` quota, 0–1024 KB. |

A permission the manifest does not hold is an error the app sees when it
calls the function — not a silent empty answer.

## The module

The module exports one function:

```js
export function page({ name }) {
  return {
    title: "Gas",
    subtitle: "Read through the shell's RPC broker",
    sections: [
      { stats: [{ label: "Ethereum gas", value: "1.02 gwei" }] },
      {
        title: "Networks",
        table: {
          columns: [{ title: "Chain" }, { title: "Gas (gwei)", align: "end" }],
          rows: [["Ethereum", "1.02"], ["Base", "0.01"]],
        },
      },
      { links: [{ title: "Settings help", page: "help" }] },
      { text: "Plain text, drawn by the shell." },
    ],
  };
}
```

`page({ name })` is called when the user opens a page; `name` is the page from
the manifest's `navigation` (or from a `links` entry). Hooks are synchronous
in this build: return the object, do not return a promise.

A thrown error is shown on the page as the app's error, and the runtime is
restarted on the next call if the call timed out or hit the memory limit.

## Page blocks

A page has `title`, `subtitle` and up to 32 `sections`. A section may hold one
of each block, drawn in this order: `stats`, `table`, `cards`, `links`, `text`,
`divider`, `chart`, `gauge`.

| Block | Shape | Limits |
|---|---|---|
| `stats` | `[{ label, value, trend, sparkline }]` | 12 per section; label 40, value 80 characters; `trend` is `"up"` or `"down"` and colors the value; `sparkline` is up to 32 finite numbers, oldest first, drawn as the shape behind the number |
| `table` | `{ columns: [{ title, align }], rows: [[…]] }` | 8 columns, 200 rows, 200 characters per cell; `align` is `start` (default) or `end`; every row has one cell per column |
| `cards` | `[{ title, subtitle, body }]` | 32 per section; body 500 characters |
| `links` | `[{ title, page }]` | 16 per section; `page` must be a page the app draws |
| `text` | a string | 2000 characters |
| `divider` | `true` | — |
| `chart` | `{ kind, points: [{ label, value }] }` | 60 points; `kind` is `line` (default) or `bar`; values are finite numbers |
| `gauge` | `{ label, value, text }` | `value` is 0 to 100; `text` is the short reading beside the label, 20 characters |

All strings are plain text. Control characters and bidirectional overrides
are refused, so a page cannot reorder or hide what it shows. A page larger
than 256 KB is refused.

## Host functions

The global `host` is the app's whole world. A function not listed here does
not exist.

### `host.setting(key)`

Returns the string the user saved for a declared setting, or `""`. A key that
is not in the manifest is an error. A `secret` setting is stored in the OS
keychain by the shell; the app only ever reads it through this call.

### `host.kv.get(key)` / `host.kv.put(key, value)` / `host.kv.delete(key)`

A small JSON store, quota from `permissions.storageKb`. `get` returns the
value or `undefined`; `put` stores any JSON-serialisable value. Without the
permission, `put` is an error and `get` returns `undefined`.

### `host.fetch(url, init?)`

An HTTP request through the shell. `init` may hold `method`, `headers` and
`body`; the methods are GET, POST, PUT, PATCH, DELETE and HEAD, and the only
headers allowed are `accept`, `content-type` and `authorization`. The answer
is `{ status, ok, headers, body }` — `body` is text, so `JSON.parse` it.

The URL must be `https` (or `http` on loopback) and its host must be in
`permissions.network`. Redirects are followed only to allowlisted hosts. The
request times out after 30 seconds and the answer is capped at 8 MB. Every
fetch is logged in the app's log.

### `host.rpc(chain, method, params?)`

A JSON-RPC read on a chain named in `permissions.chains`. The shell picks the
endpoint and pins it to the chain, so the app never names a URL. The answer is
the JSON-RPC result as a value: a hex quantity is a string, a block is an
object.

Methods that sign or send are refused before the request leaves the app:
`eth_sendTransaction`, `eth_sendRawTransaction`, `eth_signTransaction`,
anything starting with `personal_`, `wallet_` or `sign`, and the equivalent
send methods. An app reads the chain; the shell signs.

### `host.accounts()`

The addresses the user watches — `{ address, label, family }` — when the app
holds the `balances` read scope. Addresses only; there is no key here.

### `host.requestSign(request)`

Asks the shell to sign and broadcast an EVM transaction. Needs `sign` and the
chain in `permissions.chains`. The request is
`{ chainId, from?, to, value?, data?, gas?, nonce?, maxFeePerGas?, maxPriorityFeePerGas?, description? }`;
unknown keys are an error. The shell decodes the transaction, shows its own
approval window, and returns `{ hash }` only when the user approved. A
rejection is an error the app sees. There is no auto-approve path.

### `host.requestNearSign(request)`

The NEAR counterpart. The request names `chainId` (`near:testnet` or
`near:mainnet`), `receiver`, `description` and `actions`. An action's `type`
is `transfer` (with `amount` in yoctoNEAR), `functionCall` (with `method`,
`args`, `gas` and `deposit`) or `addFunctionCallKey` (with `publicKey`,
`receiverId`, `methodNames` and `allowance`). Amounts are decimal strings, so
a JavaScript number's precision cannot round a yoctoNEAR value. The shell
shows the same kind of decoded approval — including the signing key's
on-chain limit — and returns the transaction hash.

### `host.log(level, message)`

Writes to the app's log, shown under **Logs** in the app's settings. `level`
is `debug`, `info`, `warn` or `error`.

### `host.i18n(key, ...args)`

Looks a string up in the shell's translations. Unknown keys come back as the
key itself, so an app can ship its own English text and add translations
later.

### `host.render(spec)`

Validates a page object and returns it, so a helper that builds part of a page
can fail early with the shell's own error instead of at the end of `page()`.

## What an app cannot do

- **No DOM, no HTML, no CSS.** There is no `document`, no `window`, no
  `fetch`, no `XMLHttpRequest`. Pages are data, drawn by native UI.
- **No keys, no keychain, no filesystem.** `host.accounts()` returns
  addresses; signing is a request the user approves in a window the shell
  draws.
- **No approval, ever.** An app cannot draw or dismiss an approval prompt,
  open a window, download a file, or run another program.
- **No raw network.** Every host and chain is named in the manifest and shown
  to the user before install.

## Limits

- 256 MB of JavaScript memory per app; a call that passes it stops the
  runtime, which is rebuilt on the next call.
- 30 seconds per hook call, excluding time the user spends in an approval
  window. A call that passes it is interrupted.
- A plugin folder is at most 200 files, 8 MB in total and 2 MB per file; the
  main module is at most 1 MB; `plugin.json` is at most 64 KB.

## Install, publish, sign

- **Official examples**: Gas and Fear & Greed are copied in on first launch
  from the embedded copies of `testdata/examples`, and the launcher labels
  them **Official**. Their files are checked against that embedded copy every
  time the shell reads them; a copy that changed shows as broken instead of
  running. Removing one leaves it removed. Folder, zip, and store installs
  keep their own rules.
- **From a folder**: Apps → **Install from folder…**. The permission prompt
  shows what the app may do; a folder install is marked **unsigned**.
- **From a zip**: Apps → **Install from zip…**. A single top-level folder is
  stripped, so a GitHub archive works as it comes.
- **From the store**: Apps → **Store** reads the registry URL set in
  Settings and installs signed archives. See
  [`docs/store.md`](store.md) for the registry format and for
  `tools/pluginsign`, which packs, hashes and signs a folder:

```sh
go run ./tools/pluginsign keygen -out publisher.key
go run ./tools/pluginsign pack -key publisher.key -dir my-app \
  -url https://example.com/plugins/my-app-1.0.0.zip \
  -zip my-app-1.0.0.zip -entry my-app.json
go run ./tools/pluginsign registry -name "My plugins" -out registry.json my-app.json
```

A signed app's files are checked against the registry's hash every time the
shell reads the plugins folder: a signed app whose files changed shows as
broken instead of running.

## Trying it

Gas and Fear & Greed are already on the Apps launcher after a first launch.
**Open** shows the page; **App settings** shows the switch, pages and log.

To start a new app from the gas example, copy it and give it a new id (the
shell already uses `com.example.gas`):

```sh
mkdir -p ~/my-app
cp internal/plugin/testdata/examples/gas/* ~/my-app/
```

Then change `id` in `plugin.json` and Apps → **Install from folder…** →
`~/my-app`. A folder install is marked **unsigned**. The app appears on the
launcher beside the official examples; **Open** shows its page, **App
settings** shows its switch, pages, settings form and log.
