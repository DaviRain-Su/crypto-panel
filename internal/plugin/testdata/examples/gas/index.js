// Gas: two EVM chains through host.rpc. The manifest lists the chains; the
// shell picks the endpoint and pins it.

const CHAINS = [
  { id: "eip155:1", name: "Ethereum" },
  { id: "eip155:8453", name: "Base" },
];

// gwei turns a hex wei quantity into a short decimal string.
function gwei(hex) {
  const wei = BigInt(hex);
  const whole = wei / 1000000000n;
  const frac = String(wei % 1000000000n).padStart(9, "0").slice(0, 2);
  return whole + "." + frac;
}

export function page({ name }) {
  const rows = [];
  let headline = "—";
  for (const chain of CHAINS) {
    try {
      const price = host.rpc(chain.id, "eth_gasPrice");
      const block = host.rpc(chain.id, "eth_blockNumber");
      rows.push([chain.name, gwei(price), String(BigInt(block))]);
      if (chain.id === "eip155:1") headline = gwei(price);
    } catch (err) {
      rows.push([chain.name, "—", String(err.message)]);
    }
  }
  return {
    title: "Gas",
    subtitle: "Read through the shell's RPC broker",
    sections: [
      { stats: [{ label: "Ethereum gas", value: headline + " gwei" }] },
      {
        title: "Networks",
        table: {
          columns: [
            { title: "Chain" },
            { title: "Gas (gwei)", align: "end" },
            { title: "Block", align: "end" },
          ],
          rows,
        },
      },
      {
        text: "This plugin may name eip155:1 and eip155:8453. It cannot name a URL, and the shell refuses any method that signs or sends.",
      },
    ],
  };
}
