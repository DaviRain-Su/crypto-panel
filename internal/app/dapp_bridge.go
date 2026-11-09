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
      if (!p || msg.capability !== p.capability || msg.capability !== capability) return;
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
    name: "Lintel",
    connect: () => request("connect", {}),
    signIn: (params) => request("connect", params || {}),
    disconnect: () => request("disconnect", {}),
    signOut: () => request("disconnect", {}),
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
