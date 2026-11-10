// Replace this with a testnet account you control before recording a live demo.
const RECEIVER = "bob.testnet";
const AMOUNT = "1000000000000000000000"; // 0.001 NEAR in yoctoNEAR.

function request(receiver, actions, description) {
  try {
    const result = host.requestNearSign({
      chainId: "near:testnet",
      receiver,
      description,
      actions,
    });
    return {
      title: "Transaction submitted",
      subtitle: "The native wallet approved this request.",
      sections: [
        { stats: [{ label: "Transaction hash", value: result.hash }] },
        { text: "Check this exact hash on NEAR Testnet. The app cannot read a private key or approve its own request." },
      ],
    };
  } catch (err) {
    return {
      title: "Request not signed",
      subtitle: "The shell refused the request or the user rejected it.",
      sections: [{ text: String(err.message || err) }],
    };
  }
}

export function page({ name }) {
  if (name === "transfer") {
    return request(RECEIVER, [{ type: "transfer", amount: AMOUNT }], "Send 0.001 testnet NEAR from the approval demo");
  }
  if (name === "limited") {
    return request("guest-book.testnet", [{
      type: "functionCall",
      method: "unauthorized_demo_method",
      args: "{}",
      gas: "30000000000000",
      deposit: "0",
    }], "Demonstrate a function-call key boundary; do not approve an unintended call");
  }
  return {
    title: "Agent requests. You decide.",
    subtitle: "A fixed example of the same package and signing API available to agent-created apps.",
    sections: [
      { cards: [
        { title: "1. Preview", body: "A generated candidate can render native pages, but preview refuses signing and does not install it." },
        { title: "2. Review", body: "Installation needs a separate native permission review. This example requests NEAR Testnet signing only." },
        { title: "3. Approve or reject", body: "Opening a request page asks the native wallet to sign. Rejecting returns a visible error without a transaction hash." },
      ] },
      { links: [
        { title: "Request a 0.001 NEAR transfer", page: "transfer" },
        { title: "Try a restricted function-call key", page: "limited" },
      ] },
      { text: "Before a live transfer, replace RECEIVER in index.js with a testnet account you control. A full-access key does not automatically identify malicious requests: the native decode lets you decide. A restricted key enforces its receiver, methods and allowance on NEAR." },
    ],
  };
}
