# Next product slice: task and implementation log

- [x] Freeze behavior and security contracts in `next-product-technical-spec.md`.
- [x] Add tolerant 1Click icon metadata, safe remote image loading, built-in default asset art, network labels and quote-review identity.
- [ ] Read each watched EVM address across seven curated networks with network-aware snapshots and bounded refresh.
- [ ] Adapt the NEAR bridge to standard discovery, network negotiation and standard outcomes.
- [ ] Expose trend/sparkline/gauge in the Agent catalog and preview frozen pending apps without signing or installation.
- [ ] Integrate isolated topic branches, run full build/test/vet and exercise real entry points.
- [ ] Commit and prepare focused PRs without rewriting the five existing open topic PRs.

## Asset icon validation

The live 1Click token response contained 216 assets and no icon field on 2026-11-10. Built-in art therefore covers the default wNEAR/USDC pair and common BTC, ETH, USDT, DAI, BNB, AVAX and SOL symbols. NEAR art is the SVG served by near.com; other built-in PNGs are CC0 assets from spothq/cryptocurrency-icons with its license in `internal/app/assetart/LICENSE.txt`. Future API icon metadata remains supported behind the safe loader.
