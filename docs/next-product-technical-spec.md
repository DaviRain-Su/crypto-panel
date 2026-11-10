# Next product slice: PRD and technical specification

## Scope

Four independently reviewable changes, based on the latest `main` while preserving the existing topic PRs:

1. Show asset and chain identity in Intents selectors and quote review. Missing or failed icons never block quoting.
2. Read each watched EVM address on a curated set of common networks, retain per-network results, and total compatible values in USD. Signing still uses the selected network.
3. Add NEAR Connect discovery and normalize wallet return values, network negotiation and actions without weakening native approval or origin capabilities.
4. Synchronize the Agent page catalog with trends, sparklines and gauges, and expose a signing-disabled preview of a frozen pending candidate.

Out of scope: scanning every EVM chain ever deployed, wet Intents deposits, automatic wallet transactions, NEP-413/offline signing without native implementation, or bypassing native installation approval.

## Contracts and boundaries

### Asset presentation

`intents.Token` gains an optional icon URL. Only HTTPS icons are fetched, with bounded response bytes, image decode validation, timeout and an in-memory cache. The selected asset has a 32 DIP image beside its picker; unavailable images use a deterministic monogram. A small network badge distinguishes the same symbol on different chains. Quote review uses the same identity component. No network request runs synchronously during drawing.

### Multi-EVM portfolio

The registry adds Arbitrum One, Optimism, Polygon, BNB Smart Chain and Avalanche C-Chain alongside Ethereum and Base. Every network has its canonical CAIP-2 ID, numeric chain ID, native decimals, RPC and curated contracts. A refresh creates one job per network with watched addresses of that family, not merely one per family. Snapshot identity is `(network ID, normalized address)`. Jobs use bounded concurrency and independent results; one failed network is incomplete, not zero. Pricing is keyed by an explicit asset identity rather than token symbol where the models are extended. Custom RPC overrides remain scoped to their selected network.

### NEAR interoperability

The existing `window.nearWallet` bridge remains for compatibility. A standards adapter announces Lintel through the documented `near-wallet-injected` event and supports account retrieval, sign-in/out and transaction forwarding. Sign-in resolves to the standard account array at the adapter boundary. Native signing preserves the full `FinalExecutionOutcome` instead of reducing it to a hash. Requested mainnet/testnet must match the selected network. Unsupported NEP-413, `verifyOwner` and offline signing return explicit unsupported errors. Legacy and current near-api-js action shapes normalize to the existing native action format. Native origin checks, random document capabilities and decoded approval remain mandatory.

### Agent preview

The catalog accepts and describes `trend`, at most 32 sparkline points and a gauge constrained to 0–100. Preview accepts only a frozen pending installation owned by the current host, revalidates package identity and declared page, and executes in the existing signing-disabled runtime. It returns page JSON without installing, approving or changing the candidate. The creator links a successful generation to preview and native review; preview is not an approval.

## Test specification

- Icons: valid metadata, malformed/non-HTTPS URLs, oversized response, timeout, decode failure, cache hit and monogram fallback.
- Portfolio: same address on two EVM networks yields two snapshots; all curated networks are queried; native symbols/decimals stay per network; partial RPC failure preserves successful rows and marks totals incomplete; price identity does not conflate equal symbols.
- NEAR: discovery event shape, account-array adapter result, full execution outcome, network mismatch, legacy/current actions, unsupported methods, disconnected requests, origin and approval tests remain green.
- Agent: catalog accepts all new blocks and rejects invalid values; pending-only preview succeeds; installed/unknown/tampered candidates fail; signing is unavailable; preview does not install or approve.

Each topic receives focused package tests, `go build ./...`, `go test ./...`, and `go vet ./...`. GUI checks exercise the real Intents page, portfolio, a NEAR Connect example and the creator flow where the environment permits.
