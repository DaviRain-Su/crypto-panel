# The plugin store

The app's Apps page has two tabs: **Installed** (the launcher) and **Store**.
The store reads a *registry* — one static JSON file that lists signed plugins
— and installs an entry with one click, after showing what the plugin may do.
A plugin can also be installed from a local folder or zip; those installs are
unsigned and the permission prompt says so.

The app ships no catalog of its own: until a registry is set, the Store tab
says so and points at Settings. A registry is one HTTPS URL in
**Settings → Plugin registry**; `http://127.0.0.1` and `http://localhost` are
allowed for development.

## A registry

```json
{
  "version": 1,
  "name": "My plugins",
  "plugins": [
    {
      "id": "com.example.portfolio",
      "name": "Portfolio",
      "version": "1.2.0",
      "description": "What it shows, in one sentence.",
      "author": "Example",
      "url": "https://example.com/plugins/portfolio-1.2.0.zip",
      "hash": "sha256:8ba5…",
      "signature": "ed25519:lHCm…",
      "publisher": "ed25519:dnFh…"
    }
  ]
}
```

- `url` is an HTTPS URL to a zip of the plugin folder. A GitHub archive works:
  a single top-level folder is stripped when the zip is unpacked.
- `hash` is `sha256:` over the plugin's files, sorted by path. Each file is
  written as its slash-separated path, a NUL, its size in decimal, a NUL, and
  its bytes. The shell's own files (`state.json`, `kv.json`, `install.json`)
  and file-manager junk (`.DS_Store`, `Thumbs.db`, `__MACOSX/`) are not part
  of it.
- `signature` is `ed25519:` over the hash string (`sha256:…`), base64.
- `publisher` is the `ed25519:` public key that made the signature, base64.
- The archive's `plugin.json` must name the same `id` and `version` as the
  entry. Unknown fields in the registry are ignored; `version` must be 1.

The app also refuses:

- an archive whose unpacked tree does not match `hash`, or whose signature
  does not verify under `publisher`;
- an archive that escapes its folder, contains a symlink or a special file,
  or brings more than 200 files / 8 MB / 2 MB per file;
- an update to an older version, or to the same version with other contents;
- an update signed by a different publisher key than the installed one.

An install is recorded in the plugin's folder as `install.json` (source, hash,
signature, publisher). The shell checks the digest again whenever it reads the
plugins folder: a signed plugin whose files changed shows as broken instead of
running.

## Signing a plugin

`tools/pluginsign` packs and signs a folder:

```sh
go run ./tools/pluginsign keygen -out publisher.key
go run ./tools/pluginsign pack \
  -key publisher.key \
  -dir internal/plugin/testdata/examples/gas \
  -url https://example.com/plugins/gas-1.0.0.zip \
  -zip gas-1.0.0.zip -entry gas.json
go run ./tools/pluginsign registry -name "My plugins" -out registry.json gas.json
```

`keygen` prints the publisher key and writes the private key (keep it secret;
it never goes into the registry). `pack` writes the zip and the entry JSON.
`registry` combines entries into the catalog. Serve the zip and the JSON from
any HTTPS host — a static site, GitHub Pages, an object store.

The app trusts no publisher in advance: an entry's `publisher` key is the one
that entry's plugin is installed with, and an update must be signed by the same
key. There is no built-in registry and no embedded archive.

## What the user sees

Before anything is installed, the shell shows its own permission prompt: the
plugin's name, author and description, then what it may do — network hosts,
chains, watched addresses and balances, signing, storage, settings. An update
lists the permissions that are new. A local folder install is marked
**unsigned**; a store install is signed and the app says so on the plugin's
card.

A plugin that can sign still needs a separate, decoded approval for every
transaction. The store's signature says where the code came from; it never
grants a key.
