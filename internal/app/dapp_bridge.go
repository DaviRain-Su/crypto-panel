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
  if (window.top !== window) return;
  const pending = new Map();
  const send = window.fetch.bind(window);
  const stringify = JSON.stringify;
  const makeTimer = window.setTimeout.bind(window);
  const stopTimer = window.clearTimeout.bind(window);
  const Abort = window.AbortController;
  const doc = window.document;
  const origin = window.location.origin;
  const bytes = new Uint8Array(32);
  window.crypto.getRandomValues(bytes);
  const marker = Array.from(bytes, x => x.toString(16).padStart(2, "0")).join("");
  let capability = "", epoch = 0, alive = true, seq = 0;
  Object.defineProperty(window, "` + dappDocumentKey + `", { value: marker });
  Object.defineProperty(window, "` + dappBindKey + `", { value: (token, generation, documentMarker, expectedOrigin) => {
    if (documentMarker !== marker || expectedOrigin !== origin || window.document !== doc ||
        window.location.origin !== origin || generation <= epoch) return;
    epoch = generation;
    capability = token;
    alive = true;
    pending.forEach(p => p.start());
  }});
  if (window.mygo && mygo.on) {
    mygo.on("cryptopanel:near", (msg) => {
      const p = pending.get(msg.id);
      // An event that names another origin belongs to another document; a
      // late answer after navigation must not be readable here.
      if (!p || msg.origin !== origin || msg.capability !== p.capability || msg.capability !== capability) return;
      pending.delete(msg.id);
      stopTimer(p.timer);
      if (msg.error) p.reject(new Error(msg.error));
      else p.resolve(msg.result);
    });
  }
  window.addEventListener("pagehide", () => {
    alive = false;
    capability = "";
    pending.forEach(p => {
      p.controller.abort();
      stopTimer(p.timer);
      p.reject(new Error("the wallet document was closed"));
    });
    pending.clear();
  });
  function request(method, params) {
    return new Promise((resolve, reject) => {
      if (!alive) { reject(new Error("the wallet document was closed")); return; }
      const id = "r" + (++seq) + "-" + marker.slice(0, 16);
      const controller = new Abort();
      const p = { resolve, reject, controller, started: false, capability: "" };
      p.timer = makeTimer(() => {
        if (pending.delete(id)) {
          controller.abort();
          reject(new Error("the wallet did not answer"));
        }
      }, 120000);
      p.start = () => {
        if (p.started || !capability || !alive) return;
        p.started = true;
        p.capability = capability;
        send("` + dappRequestURL + `", {
          method: "POST",
          mode: "no-cors",
          signal: controller.signal,
          body: stringify({ id, method, params: params || {}, capability: p.capability }),
        }).catch((err) => {
          if (pending.delete(id)) { stopTimer(p.timer); reject(err); }
        });
      };
      pending.set(id, p);
      p.start();
    });
  }
  function base64Bytes(bytes) {
    let binary = "";
    for (const byte of bytes) binary += String.fromCharCode(byte);
    return window.btoa(binary);
  }
  function normalizeArgs(args) {
    if (args instanceof ArrayBuffer) args = new Uint8Array(args);
    if (ArrayBuffer.isView(args)) args = new Uint8Array(args.buffer, args.byteOffset, args.byteLength);
    if (args instanceof Uint8Array) {
      const text = new TextDecoder().decode(args);
      try { JSON.parse(text); return text; } catch { return "base64:" + base64Bytes(args); }
    }
    if (args != null && typeof args !== "string") return JSON.stringify(args);
    return args;
  }
  function normalizeActions(actions) {
    if (!Array.isArray(actions)) throw new Error("actions must be an array");
    return actions.map((a) => {
      if (!a || typeof a !== "object") throw new Error("each action must be an object");
      // @near-js/transactions action instances use lower-case enum fields.
      if (a.transfer) return { type: "transfer", amount: String(a.transfer.deposit) };
      if (a.functionCall) {
        const call = a.functionCall;
        return { type: "functionCall", method: call.methodName, args: normalizeArgs(call.args),
          gas: String(call.gas), deposit: String(call.deposit) };
      }
      if (a.addKey) {
        const key = a.addKey, permission = key.accessKey.permission;
        if (!permission.functionCall) throw new Error("only function-call access keys are supported");
        a = { type: "AddKey", params: { publicKey: key.publicKey.toString(), accessKey: { permission: permission.functionCall } } };
      }
      const type = String(a.type || "").toLowerCase();
      const p = a.params || {};
      if (type === "transfer") {
        return { type: "transfer", amount: String(a.amount != null ? a.amount : (p.deposit != null ? p.deposit : "0")) };
      }
      if (type === "functioncall") {
        return {
          type: "functionCall", method: a.method || p.methodName || "",
          args: normalizeArgs(a.args != null ? a.args : (p.args != null ? p.args : "")),
          gas: a.gas != null ? String(a.gas) : (p.gas != null ? String(p.gas) : ""),
          deposit: a.deposit != null ? String(a.deposit) : (p.deposit != null ? String(p.deposit) : ""),
        };
      }
      if (type === "addkey" || type === "addfunctioncallkey") {
        const accessKey = p.accessKey || a.accessKey || {};
        const fc = accessKey.permission && typeof accessKey.permission === "object" ?
          (accessKey.permission.functionCall || accessKey.permission.FunctionCall || accessKey.permission) :
          (accessKey.FunctionCall || accessKey.functionCall || {});
        const allowance = fc.allowance != null ? fc.allowance : a.allowance;
        return {
          type: "addFunctionCallKey", publicKey: String(a.publicKey || p.publicKey || ""),
          receiverId: fc.receiverId || fc.receiver_id || a.contractId || "",
          methodNames: fc.methodNames || fc.method_names || a.methodNames || [],
          allowance: allowance != null ? String(allowance) : "",
        };
      }
      throw new Error("action type " + (a.type || "unknown") + " is unsupported");
    });
  }
  function transactionParams(params) {
    return {
      network: params.network || selectedNetwork || undefined,
      receiverId: params.receiverId,
      from: params.signerId || params.from,
      description: params.description,
      actions: normalizeActions(params.actions),
    };
  }
  let selectedNetwork = "";
  function currentNetwork() {
    return request("getNetwork", {}).then(result => {
      selectedNetwork = result.networkId;
      return result;
    });
  }
  async function requireNetwork(requested) {
    const current = await currentNetwork();
    if (requested && requested !== current.networkId) {
      throw new Error("network " + requested + " does not match selected " + current.networkId);
    }
  }
  async function signAndSendTransaction(params) {
    await requireNetwork(params.network);
    return request("signAndSendTransaction", transactionParams(params));
  }
  window.nearWallet = {
    isNearWallet: true,
    name: "Lintel",
    connect: () => request("connect", {}),
    signIn: (params) => currentNetwork().then(() => request("connect", params || {})),
    disconnect: () => request("disconnect", {}),
    signOut: () => request("disconnect", {}),
    getAccounts: () => request("getAccounts", {}),
    getNetwork: () => request("getNetwork", {}),
    signAndSendTransaction,
    signAndSendTransactions: async (params) => {
      const results = [];
      for (const transaction of params.transactions || []) {
        results.push(await signAndSendTransaction({
          network: params.network, signerId: params.signerId, ...transaction,
        }));
      }
      return results;
    },
    addFunctionCallKey: (params) => request("addFunctionCallKey", params || {}),
  };

  const manifest = {
    id: "lintel", version: "1.0.0", name: "Lintel",
    description: "Local-first wallet with native transaction approval",
    icon: "https://raw.githubusercontent.com/davirain-su/lintel/main/resources/icon.png",
    website: "https://github.com/davirain-su/lintel", executor: "", type: "injected",
    platform: { web: "https://github.com/davirain-su/lintel" },
    permissions: {},
    features: {
      signMessage: false, signTransaction: false, signAndSendTransaction: true,
      signAndSendTransactions: true, signInWithoutAddKey: true,
      signInAndSignMessage: false, signInWithFunctionCallKey: false,
      signDelegateActions: false, verifyOwner: false, mainnet: true, testnet: true,
    },
  };
  function unsupported(name) {
    return () => Promise.reject(new Error(name + " is unsupported: Lintel does not implement offline or NEP-413 signing"));
  }
  const adapter = {
    manifest,
    signIn: async (params) => {
      params = params || {};
      if (params.addFunctionCallKey) throw new Error("addFunctionCallKey during signIn is unsupported");
      const result = await request("connect", { network: params.network });
      return result.accounts;
    },
    signOut: () => request("disconnect", {}),
    getAccounts: (params) => request("getAccounts", { network: params && params.network }),
    signAndSendTransaction,
    signAndSendTransactions: (params) => request("signAndSendTransactions", {
      network: params.network, signerId: params.signerId,
      transactions: (params.transactions || []).map(transactionParams),
    }),
    signMessage: unsupported("signMessage"), verifyOwner: unsupported("verifyOwner"),
    signTransaction: unsupported("signTransaction"), signTransactions: unsupported("signTransactions"),
    signDelegateActions: unsupported("signDelegateActions"),
    signInAndSignMessage: unsupported("signInAndSignMessage"),
  };
  window.addEventListener("near-selector-ready", () => {
    window.dispatchEvent(new window.CustomEvent("near-wallet-injected", { detail: adapter }));
  });
})();
`
