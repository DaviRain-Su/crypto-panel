package app

// nearBridgeScript is injected into every DApp page before its own scripts.
// It gives the page a small NEAR wallet object and nothing else: no Go
// methods, no keychain, no files. A request goes to the shell's custom
// scheme; the answer comes back as an event, because an untrusted page
// cannot read the response of another origin.
//
// The page can ask; it cannot approve. Every signature still opens the
// shell's own window.
const nearBridgeScript = `(() => {
  if (window.nearWallet) return;
  const pending = new Map();
  let seq = 0;
  if (window.mygo && mygo.on) {
    mygo.on("cryptopanel:near", (msg) => {
      const p = pending.get(msg.id);
      if (!p) return;
      pending.delete(msg.id);
      clearTimeout(p.timer);
      if (msg.error) p.reject(new Error(msg.error));
      else p.resolve(msg.result);
    });
  }
  function request(method, params) {
    return new Promise((resolve, reject) => {
      const id = "r" + (++seq) + "-" + Math.random().toString(36).slice(2);
      const timer = setTimeout(() => {
        if (pending.delete(id)) reject(new Error("the wallet did not answer"));
      }, 120000);
      pending.set(id, { resolve: resolve, reject: reject, timer: timer });
      fetch("cryptopanel://near/request", {
        method: "POST",
        mode: "no-cors",
        body: JSON.stringify({ id: id, method: method, params: params || {} }),
      }).catch((err) => {
        if (pending.delete(id)) { clearTimeout(timer); reject(err); }
      });
    });
  }
  function normalizeActions(actions) {
    return (actions || []).map((a) => {
      const type = String(a.type || "").toLowerCase();
      const p = a.params || {};
      if (type === "transfer") {
        return { type: "transfer", amount: String(a.amount != null ? a.amount : (p.deposit != null ? p.deposit : "0")) };
      }
      if (type === "functioncall") {
        let args = a.args != null ? a.args : p.args;
        if (args == null) args = "";
        if (typeof args !== "string") args = JSON.stringify(args);
        const gas = a.gas != null ? a.gas : p.gas;
        const deposit = a.deposit != null ? a.deposit : p.deposit;
        return {
          type: "functionCall",
          method: a.method || p.methodName || "",
          args: args,
          gas: gas != null ? String(gas) : "",
          deposit: deposit != null ? String(deposit) : "",
        };
      }
      if (type === "addkey" || type === "addfunctioncallkey") {
        const accessKey = p.accessKey || a.accessKey || {};
        const fc = accessKey.FunctionCall || accessKey.functionCall || {};
        const allowance = fc.allowance != null ? fc.allowance : a.allowance;
        return {
          type: "addFunctionCallKey",
          publicKey: a.publicKey || p.publicKey || "",
          receiverId: fc.receiverId || fc.receiver_id || a.contractId || "",
          methodNames: fc.methodNames || fc.method_names || a.methodNames || [],
          allowance: allowance != null ? String(allowance) : "",
        };
      }
      return a;
    });
  }
  window.nearWallet = {
    isNearWallet: true,
    name: "Crypto Panel",
    connect: () => request("connect", {}),
    signIn: (params) => request("connect", params || {}),
    getAccounts: () => request("getAccounts", {}),
    getNetwork: () => request("getNetwork", {}),
    signAndSendTransaction: (params) => request("signAndSendTransaction", {
      receiverId: params.receiverId,
      from: params.signerId || params.from,
      description: params.description,
      actions: normalizeActions(params.actions),
    }),
    addFunctionCallKey: (params) => request("addFunctionCallKey", params || {}),
  };
})();
`
