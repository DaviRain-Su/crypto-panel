(() => {
  "use strict";

  const params = new URLSearchParams(location.hash.slice(1));
  const mode = params.get("mode");
  const state = params.get("state") || "";
  const challenge = decodeB64url(params.get("challenge") || "");

  const title = document.getElementById("title");
  const lead = document.getElementById("lead");
  const site = document.getElementById("site");
  const status = document.getElementById("status");
  const continueButton = document.getElementById("continue");
  const cancelButton = document.getElementById("cancel");
  let finished = false;
  let controller = null;

  site.textContent = location.hostname;

  function decodeB64url(value) {
    const s = value.replace(/-/g, "+").replace(/_/g, "/");
    const padded = s + "=".repeat((4 - (s.length % 4)) % 4);
    const bin = atob(padded);
    const out = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) {
      out[i] = bin.charCodeAt(i);
    }
    return out;
  }

  function encodeB64url(value) {
    const bytes = value instanceof Uint8Array ? value : new Uint8Array(value);
    let bin = "";
    for (let i = 0; i < bytes.length; i++) {
      bin += String.fromCharCode(bytes[i]);
    }
    return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  }

  // 当前版本仅支持应用的 localhost 服务，
  // 没有托管 HTTPS 页面或用于 Passkey 结果的自定义 scheme 回调。
  async function deliver(result) {
    if (finished) return;
    try {
      const res = await fetch("/result", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(result),
      });
      if (!res.ok) {
        throw new Error("Lintel did not accept the result. Start a new operation in the app.");
      }
      finished = true;
      status.classList.toggle("error", Boolean(result.error));
      status.textContent = result.error
        ? "The passkey operation ended. Return to Lintel to start again."
        : "Done. You can close this tab.";
      continueButton.disabled = true;
      cancelButton.disabled = true;
    } catch (err) {
      stop(err && err.message ? err.message : "Lintel did not accept the result.");
    }
  }

  async function callback(result) {
    await deliver(result);
  }

  async function fail(code, message) {
    await deliver({ error: code, message, state });
  }

  function stop(message) {
    status.textContent = message;
    status.classList.add("error");
    continueButton.disabled = true;
  }

  if (mode !== "create" && mode !== "get") {
    title.textContent = "Invalid link";
    lead.textContent = "This page was opened without a passkey request.";
    stop("Open the passkey flow from Lintel.");
    return;
  }

  if (location.hostname !== "localhost" || location.protocol !== "http:" || !window.isSecureContext) {
    title.textContent = "Open from Lintel";
    lead.textContent = "This build uses its own localhost passkey page.";
    stop("Start the operation in the app. Hosted HTTPS callbacks are not supported.");
    return;
  }

  if (!window.PublicKeyCredential || !navigator.credentials) {
    title.textContent = "Passkeys not available";
    lead.textContent = "This browser does not support WebAuthn.";
    stop("Use a browser with passkey support.");
    return;
  }

  const host = location.hostname;

  if (mode === "create") {
    title.textContent = "Create a passkey";
    lead.textContent = "Lintel will use this passkey to approve transactions for your smart account.";
  } else {
    title.textContent = "Confirm with your passkey";
    lead.textContent = "Confirm access to your passkey for the operation you started in Lintel.";
  }

  async function run() {
    if (finished) return;
    controller = new AbortController();
    continueButton.disabled = true;
    status.classList.remove("error");
    status.textContent = "Waiting for your passkey…";
    try {
      if (mode === "create") {
        const credential = await navigator.credentials.create({
          signal: controller.signal,
          publicKey: {
            challenge,
            rp: {
              id: host,
              name: params.get("rpName") || "Lintel",
            },
            user: {
              id: decodeB64url(params.get("userId") || ""),
              name: params.get("userName") || "lintel",
              displayName: params.get("userDisplayName") || params.get("userName") || "Lintel",
            },
            pubKeyCredParams: [{ type: "public-key", alg: -7 }],
            authenticatorSelection: {
              residentKey: "required",
              requireResidentKey: true,
              userVerification: "required",
            },
            attestation: "none",
            timeout: 60000,
          },
        });
        if (!credential) {
          throw new Error("No credential was created.");
        }
        const response = credential.response;
        await callback({
          kind: "create",
          state,
          credentialId: encodeB64url(credential.rawId),
          clientDataJSON: encodeB64url(response.clientDataJSON),
          attestationObject: encodeB64url(response.attestationObject),
          transports: response.getTransports ? response.getTransports() : [],
        });
      } else {
        const credentialId = params.get("credentialId");
        const assertion = await navigator.credentials.get({
          signal: controller.signal,
          publicKey: {
            challenge,
            rpId: host,
            allowCredentials: credentialId
              ? [{ type: "public-key", id: decodeB64url(credentialId) }]
              : [],
            userVerification: "required",
            timeout: 60000,
          },
        });
        if (!assertion) {
          throw new Error("No assertion was returned.");
        }
        const response = assertion.response;
        await callback({
          kind: "get",
          state,
          credentialId: encodeB64url(assertion.rawId),
          clientDataJSON: encodeB64url(response.clientDataJSON),
          authenticatorData: encodeB64url(response.authenticatorData),
          signature: encodeB64url(response.signature),
          userHandle: response.userHandle ? encodeB64url(response.userHandle) : "",
        });
      }
    } catch (err) {
      if (!finished && !(controller && controller.signal.aborted)) {
        const message = err && err.message ? err.message : "The passkey operation failed.";
        stop(message);
        await fail(err && err.name ? err.name : "Failed", message);
      }
    }
  }

  continueButton.addEventListener("click", run);
  cancelButton.addEventListener("click", () => {
    if (controller) controller.abort();
    fail("Canceled", "The passkey ceremony was canceled.");
  });
  window.addEventListener("pagehide", () => {
    if (finished) return;
    if (controller) controller.abort();
    navigator.sendBeacon("/result", new Blob([
      JSON.stringify({ error: "Canceled", message: "The passkey page was closed.", state }),
    ], { type: "application/json" }));
  });
})();
