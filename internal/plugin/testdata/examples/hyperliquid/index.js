export function page() {
  const mids = host.hyperliquid.info({ type: "allMids" });
  const book = host.hyperliquid.info({ type: "l2Book", coin: "BTC", nSigFigs: 5 });
  const bids = book.levels[0] || [];
  const asks = book.levels[1] || [];
  const rows = [];
  for (let i = 0; i < Math.min(5, bids.length, asks.length); i++) {
    rows.push([bids[i].px, bids[i].sz, asks[i].px, asks[i].sz]);
  }
  return {
    title: "Hyperliquid markets",
    subtitle: "Public mainnet Info reads. No wallet or signing permission.",
    sections: [
      { stats: [
        { label: "BTC mid", value: mids.BTC || "Unavailable" },
        { label: "ETH mid", value: mids.ETH || "Unavailable" },
      ] },
      { title: "BTC order book", table: {
        columns: [
          { title: "Bid" }, { title: "Size", align: "end" },
          { title: "Ask" }, { title: "Size", align: "end" },
        ],
        rows,
      } },
      { text: "HyperCore, HyperEVM, spot and perps balances are distinct surfaces. This example reads markets only." },
    ],
  };
}
