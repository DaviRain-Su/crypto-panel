# Generated chain capabilities: technical and test specification

## Scope and non-goals

This slice extends the existing plugin PRD and native-shell architecture. Generated JavaScript remains declarative and sandboxed. Trusted Go code owns protocol requests, exact decimal conversion, response validation and preview isolation.

In scope: exact `host.units`; allowlisted Hyperliquid Info reads; NEAR contract view calls; NEAR Intents token discovery, signed dry quotes and execution-status reads; ephemeral candidate-preview KV; machine-readable and native preview notices; matching Agent Catalog schemas and two packaged authoring examples.

Out of scope: Hyperliquid Exchange, private keys, API/agent wallets, WebSockets, order signing, withdrawals, wet Intents quotes, deposits, transaction broadcasts, arbitrary SDK dependencies, forms, general callbacks, automatic installation and mainnet trading.

## Data and capability contracts

### Exact units

`host.units.format(baseUnitDecimalString, decimals)` returns the exact decimal string from `chain.FormatUnits`. `baseUnitDecimalString` must be a base-10 integer string; decimals is an integer from 0 through 255. JavaScript numbers and non-integral strings are rejected rather than rounded.

`host.units.parse(humanDecimalString, decimals)` returns a base-unit integer string from `chain.ParseUnits`. Signs, exponents, extra decimal points, excess fractional digits and decimals outside 0–255 are rejected. Empty input is rejected.

`host.units.formatDisplay(baseUnitDecimalString, decimals, maxFraction)` returns `chain.FormatAmount`, including grouping, half-up rounding and the existing dust marker. `maxFraction` is 0–255 and is clamped to decimals by the existing formatter.

### Hyperliquid

`host.hyperliquid.info(request)` accepts `{type, ...parameters}` with no unknown operation dispatch. `type` is one of: `meta`, `spotMeta`, `metaAndAssetCtxs`, `spotMetaAndAssetCtxs`, `allMids`, `l2Book`, `candleSnapshot`, `clearinghouseState`, `spotClearinghouseState`, `portfolio`, `openOrders`, `userFillsByTime`.

The manifest must allow `api.hyperliquid.xyz` for mainnet or `api.hyperliquid-testnet.xyz` for testnet. An optional request `testnet:true` selects testnet; no URL, Exchange action, agent key or signature is accepted. Only `POST /info` is emitted. The response body is bounded to 4 MiB, HTTP must be exactly 200, redirects are refused and the returned JSON value is passed to the plugin. The adapter uses a 25-second client timeout and the host's 30-second fetch bound.

### NEAR view

`host.near.view({chainId, contract, method, args?})` requires a declared NEAR chain. `contract` is 2–64 characters, `method` is 1–128 characters and `args` defaults to `{}`. It calls the existing pinned RPC broker with `query`, `request_type:"call_function"`, the contract account ID, base64url-free standard-base64 JSON arguments and `finality:"final"`.

JSON arguments are limited to 64 KiB and the broker response to 4 MiB. The broker response must contain a NEAR `result` byte array; the legacy base64 string representation is accepted for custom brokers, while unrelated node metadata such as logs and block height is ignored. Empty bytes return `null`; otherwise bytes must decode as one JSON value. Errors, logs and non-JSON results are not silently converted into empty objects. No signer or user-selected RPC URL exists.

### NEAR Intents

The manifest must allow `1click.chaindefuser.com`. The host exposes:

- `host.intents.tokens()` → validated token objects `{assetId,symbol,blockchain,decimals,price,icon}`.
- `host.intents.dryQuote(request)` → the existing exact quote shape plus `signature`, `timestamp` and `verified:true`.
- `host.intents.status({depositAddress,depositMemo?})` → `{status,correlationId,updatedAt,quoteResponse,swapDetails}` with a bounded status enum and verified original quote.

`dryQuote` accepts only the existing origin/destination asset IDs, positive integer base-unit amount, recipient, refund address, optional slippage basis points (1–1000, omitted/zero defaults to 100) and optional RFC3339 deadline. The Go client always sends `dry:true`, `EXACT_INPUT`, origin-chain deposit/refund and destination-chain recipient. The live quote POST accepts HTTP 200 or its observed 201 success status; tokens and status accept only 200. A dry answer containing a non-empty deposit address is rejected.

Quote verification follows `one-click-sdk-typescript` `src/quote-signature.ts`: select the 12 required signed request fields plus truthy optional routing fields, select the eight dry quote amount fields, stable-stringify with timestamp, SHA-256, Base58 and Ed25519 verification using the official manager key. Amounts remain exact decimal strings; signed protocol numbers use JavaScript number parsing and JSON.stringify notation, including its exponent thresholds. Missing/invalid signature or timestamp, a malformed key/signature, mutation and timestamps outside a five-minute clock-skew/freshness window fail closed. The official staging dry vector verifies against its published staging key; the production client uses only the production key.

Status accepts exactly `PENDING_DEPOSIT`, `KNOWN_DEPOSIT_TX`, `INCOMPLETE_DEPOSIT`, `PROCESSING`, `SUCCESS`, `REFUNDED`, `FAILED`. The nested original quote is verified by the same signature algorithm and must retain a valid RFC3339 timestamp. As an historical record, it is not subject to the dry quote's current-time freshness window. Status never submits a deposit hash or creates a quote.

### Preview state

Installed-app refresh keeps its existing durable KV behavior. Candidate preview constructs an isolated, empty in-memory KV with the candidate quota. `get`, `put` and `delete` work during that one page evaluation, including repeated writes, but no candidate preview can read, overwrite or delete the installed app's `kv.json`. Closing the runner discards the overlay.

Every successful candidate preview has `signing:"disabled"`, `storage:"ephemeral"`, `network:"declared methods allowed"`. Local API emits these as `X-Lintel-Preview-Signing`, `X-Lintel-Preview-Storage` and `X-Lintel-Preview-Network`. The native notice carries the same facts. Preview still executes declared network methods and is not described as side-effect-free.

### Catalog and examples

The catalog describes every new function, required network/chain permission, exact input/output shapes and contexts (`ui`, `apiRefresh`). It packages Hyperliquid market and NEAR Intents dry-quote examples without adding them to first-launch official installations. Examples use no signing permission, no wet quote and no real wallet address. Existing page schemas remain compatible.

## Constants and failure boundaries

- Protocol response: 4 MiB; protocol client timeout: 25 seconds.
- Decimals: 0–255; NEAR method: 1–128 characters; contract: 2–64 characters.
- Quote timestamp freshness: ±5 minutes; default slippage: 100 bps; maximum: 1000 bps.
- Hyperliquid: mainnet/testnet fixed HTTPS origins and `/info` only.
- Intents: fixed HTTPS origin, dry quote only, seven explicit status values.

At least these boundaries are tested: zero decimals, 255 decimals, negative base-unit formatting, signed parse rejection, excess fraction rejection, numeric amount rejection, unknown Hyperliquid operation, network outside manifest, redirect, non-200, oversized body, malformed JSON, NEAR non-NEAR chain, malformed result, dry deposit-address injection, missing quote signature, stale timestamp, official signature vector, quote mutation, invalid status, missing memo forwarding, preview overwrite/delete isolation and Catalog/runtime agreement.

## Task breakdown

1. Add failing protocol, host, preview, HTTP metadata and Catalog tests.
2. Implement exact units and typed host/adapter calls.
3. Implement quote verification and status reads.
4. Implement in-memory preview KV and metadata/notice.
5. Add examples and documentation.
6. Run focused, full, race, vet, build and real entry-point acceptance; review and commit only the scoped diff.

## Test and acceptance specification

Each new host function has happy-path, boundary and error/permission tests. Hyperliquid tests assert the exact request type and fixed path. Intents tests assert `dry:true`, exact amount strings, official-vector signature verification, freshness, mutation refusal and status behavior. NEAR tests assert base64 arguments and decoded results. Preview tests pre-populate installed KV, then write/delete during ACP and Local-API candidate previews and verify the durable file is unchanged. HTTP tests assert all three metadata headers. Catalog tests locate every function and both examples.

Final validation is `go test ./... -count=1`, focused race tests for plugin/app/agent/intents/Hyperliquid, `go vet ./...`, `go build ./...`, `git diff --check` and the production MyGo build. Real public read calls and a real ACP generation are acceptance checks only: they must not create wet quotes, install automatically, sign or broadcast. Network or paid-Agent limitations are reported separately from deterministic test results.
