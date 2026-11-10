export function page() {
  const tokens = host.intents.tokens();
  const rows = tokens.slice(0, 20).map((token) => [
    token.symbol,
    token.blockchain,
    String(token.decimals),
    token.assetId,
  ]);
  return {
    title: "NEAR Intents assets",
    subtitle: "Signature-verified dry quotes only; no deposit address or signing.",
    sections: [
      { stats: [{ label: "Supported assets", value: String(tokens.length) }] },
      { title: "First 20 assets", table: {
        columns: [{ title: "Asset" }, { title: "Chain" }, { title: "Decimals", align: "end" }, { title: "Asset ID" }],
        rows,
      } },
      { text: "Use host.intents.dryQuote with exact asset IDs and a base-unit decimal-string amount. Use host.units.parse for conversion. Wet quotes and deposits are not available." },
    ],
  };
}
