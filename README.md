# crypto-panel

**Desktop apps for watching crypto, in a menu bar panel — extended by sandboxed
plugins.** Watch-only by default: keys never reach a plugin, and signing (when it
comes) is an approval the shell renders itself.

Design: [`docs/design.md`](docs/design.md). Status: design, before the first line of
code.

## The idea in one screen

- **The shell** (Go + [MyGo](https://github.com/egoist/mygo) native UI): a tray item
  with the portfolio's headline, a panel of pages, a plugin manager, permission
  prompts, decoded approvals, chain brokers.
- **Plugins** (JavaScript, anyone's): a folder with `plugin.json` and one module, run
  each in its own [moejs](https://github.com/MoeForge/moejs) runtime — pure Go, one
  realm each, with memory and time limits — and only the host functions its manifest
  grants.
- **Plugin UI is data.** A plugin returns pages (`stats`, `table`, `cards`, `links`,
  `chart`); the shell draws them with native UI. No HTML, no plugin-drawn dialogs, so
  an approval prompt cannot be imitated.

## Where the pieces come from

| | |
|---|---|
| [Metrix.Bar](https://metrix.bar) | The product: a number in the menu bar, a panel on click, a dashboard as one declarative file, readable permissions, a read-only local API for agents |
| [mygo-music](https://github.com/egoist/mygo-music) | The plugin model: a folder + `plugin.json` + one module, one runtime per plugin, host-enforced `network` allowlists, declarative pages, settings as native forms, secrets in the OS keychain |
| [MyGo](https://github.com/egoist/mygo) | The substrate: `mygo.NewTray`, `mygo.NewFlyout`, `package ui`, `fetch`/`websocket`, `Secrets`, `Protocol`, `GlobalShortcut`, `ui.Tester` |

## Milestones

| | |
|---|---|
| **M0** | Shell: tray and panel, watch-only addresses, balances from one EVM RPC |
| **M1** | Plugin runtime: manifest, moejs realm, limits, `setting`/`kv`/`fetch`/`render`, plugin manager, two demo plugins |
| **M2** | Install from GitHub/zip with signature and hash, permission prompts, the malicious-plugin test suite |
| **M3** | Decoded approvals, optional hot wallet in the OS keystore, `host.requestSign` |
| **M4** | Hardware wallet, agent API, screen-edge panel and global shortcut |

## Constraints that are not negotiable

- Plugins never see keys. `host.accounts()` returns addresses; signing is
  `host.requestSign(tx)`, queued and approved with the shell's own decode.
- No `process` permission: spawning binaries in a crypto app is a backdoor.
- RPC goes through the shell's broker so endpoints are pinned, cached, rate-limited
  and audited.
- Installs are verified: `hash` and `signature` in the manifest, a strict schema, and
  new permissions on update require confirmation.

## Building (once M0 lands)

```sh
go tool mygo dev      # run in development
go tool mygo build    # package into build/
go test ./internal/...
```

The Go module path and the final name are open questions (see the design).
