const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const script = fs.readFileSync(path.join(__dirname, "ceremony.js"), "utf8");

function page({ get, hostname = "localhost", protocol = "http:", postOK = true } = {}) {
  const listeners = new Map();
  const posts = [];
  const beacons = [];
  const elements = new Map();
  const element = (id) => {
    if (!elements.has(id)) {
      elements.set(id, {
        textContent: "", disabled: false,
        classList: { add() {}, remove() {}, toggle() {} },
        addEventListener: (event, fn) => listeners.set(`${id}:${event}`, fn),
      });
    }
    return elements.get(id);
  };
  const location = {
    hostname, protocol,
    hash: "#mode=get&state=pending-state&challenge=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&credentialId=Ag",
  };
  vm.runInNewContext(script, {
    location, URLSearchParams, Uint8Array, AbortController, Blob, atob, btoa,
    document: { getElementById: element },
    window: {
      isSecureContext: true, PublicKeyCredential: class {},
      addEventListener: (event, fn) => listeners.set(`window:${event}`, fn),
    },
    navigator: {
      credentials: { get },
      sendBeacon: (url, body) => { beacons.push({ url, body }); return true; },
    },
    fetch: async (url, options) => {
      posts.push({ url, body: JSON.parse(options.body) });
      return { ok: postOK };
    },
  });
  return { listeners, posts, beacons, element, location };
}

function pendingCredential({ signal }) {
  return new Promise((resolve, reject) => {
    signal.addEventListener("abort", () => reject(Object.assign(new Error("aborted"), { name: "AbortError" })));
  });
}

test("authenticator cancellation posts the pending state and ends the wait", async () => {
  const view = page({ get: async () => { throw Object.assign(new Error("canceled"), { name: "NotAllowedError" }); } });
  await view.listeners.get("continue:click")();
  assert.equal(view.posts.length, 1);
  assert.equal(view.posts[0].body.error, "NotAllowedError");
  assert.equal(view.posts[0].body.state, "pending-state");
  assert.equal(view.element("continue").disabled, true);
  view.listeners.get("window:pagehide")();
  assert.equal(view.beacons.length, 0);
});

test("Cancel aborts the authenticator and sends one cancellation with state", async () => {
  const view = page({ get: pendingCredential });
  const run = view.listeners.get("continue:click")();
  view.listeners.get("cancel:click")();
  await run;
  assert.equal(view.posts.length, 1);
  assert.equal(view.posts[0].body.error, "Canceled");
  assert.equal(view.posts[0].body.state, "pending-state");
});

test("closing the page aborts the authenticator and beacons cancellation", async () => {
  const view = page({ get: pendingCredential });
  const run = view.listeners.get("continue:click")();
  view.listeners.get("window:pagehide")();
  await run;
  assert.equal(view.posts.length, 0);
  assert.equal(view.beacons.length, 1);
  const message = JSON.parse(await view.beacons[0].body.text());
  assert.equal(message.error, "Canceled");
  assert.equal(message.state, "pending-state");
  assert.equal(view.beacons[0].url, "/result");
});

test("a failed result POST stops without a custom-scheme fallback", async () => {
  const view = page({ postOK: false, get: async () => { throw new Error("failure"); } });
  await view.listeners.get("continue:click")();
  assert.equal(view.posts.length, 1);
  assert.equal(view.location.href, undefined);
  assert.equal(view.element("continue").disabled, true);
  assert.match(view.element("status").textContent, /Start a new operation/);
});

test("a hosted HTTPS page cannot start an app ceremony", () => {
  const view = page({ hostname: "example.com", protocol: "https:" });
  assert.equal(view.listeners.has("continue:click"), false);
  assert.match(view.element("status").textContent, /Hosted HTTPS callbacks are not supported/);
});
