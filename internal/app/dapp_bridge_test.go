package app

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// 实际执行 preload 与原生 binder，验证桥接行为。Node 不访问网络，
// fetch、计时器、事件与页面文档均为测试替身。WebKit 的 frame 隔离由
// SDK 仅注入主 frame 的 PreloadScript 和浏览器同源策略提供。
func TestDappPreloadDocumentCapabilityAndCancellation(t *testing.T) {
	runDappPreloadFixture(t, dappPreloadFixture)
}

func TestDappPreloadNearConnectCompatibility(t *testing.T) {
	runDappPreloadFixture(t, dappNearConnectFixture)
}

func runDappPreloadFixture(t *testing.T, fixture string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable for executing the preload fixture")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "-e", fixture)
	cmd.Stdin = strings.NewReader(nearBridgeScript)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("preload fixture: %v\n%s", err, out)
	}
}

const dappPreloadFixture = `
const assert = require("node:assert/strict");
const vm = require("node:vm");
const script = require("node:fs").readFileSync(0, "utf8");
const documentKey = "__cryptoPanelNearDocument", bindKey = "__cryptoPanelBindNear";
function page(origin = "https://dapp.test", frame = false) {
  const sent = [], timers = new Map(), events = new Map();
  let timerID = 0, answer;
  const w = {
    document: {}, location: { origin }, crypto: require("node:crypto").webcrypto,
    AbortController,
    fetch: (url, options) => { sent.push({ url, options, body: JSON.parse(options.body) }); return Promise.resolve({}); },
    setTimeout: (fn, ms) => { timers.set(++timerID, { fn, ms }); return timerID; },
    clearTimeout: id => timers.delete(id),
    addEventListener: (name, fn) => events.set(name, fn),
    mygo: { on: (name, fn) => { assert.equal(name, "cryptopanel:near"); answer = fn; } },
  };
  w.window = w;
  w.top = frame ? {} : w;
  vm.runInNewContext(script, w);
  return { w, sent, timers, events, answer: msg => answer(msg) };
}
(async () => {
  const iframe = page("https://attacker.test", true);
  assert.equal(iframe.w.nearWallet, undefined);
  assert.equal(iframe.w[bindKey], undefined);

  const first = page();
  const firstMarker = first.w[documentKey];
  assert.match(firstMarker, /^[0-9a-f]{64}$/);
  assert.equal(Object.getOwnPropertyDescriptor(first.w, bindKey).writable, false);
  assert.equal(Object.getOwnPropertyDescriptor(first.w, bindKey).configurable, false);
  assert.equal(Object.getOwnPropertyDescriptor(first.w, documentKey).configurable, false);
  const firstCall = first.w.nearWallet.getNetwork();
  assert.equal(first.sent.length, 0, "no capability before native binding");
  const oldToken = "a".repeat(64), token = "b".repeat(64);
  first.w[bindKey](oldToken, 1, firstMarker, "https://dapp.test");
  assert.equal(first.sent.length, 1);
  assert.equal(first.sent[0].url, "cryptopanel://near/request");
  assert.equal(first.sent[0].body.capability, oldToken);
  first.answer({ id: first.sent[0].body.id, capability: oldToken, origin: "https://dapp.test", result: "first" });
  assert.equal(await firstCall, "first");

  // 即使 origin 相同，旧文档延迟执行的原生 Eval 也不能将 token 装入新文档。
  const current = page();
  const marker = current.w[documentKey];
  assert.notEqual(marker, firstMarker);
  const call = current.w.nearWallet.getAccounts();
  current.w[bindKey](oldToken, 1, firstMarker, "https://dapp.test");
  current.w[bindKey](oldToken, 1, marker, "https://old-origin.test");
  assert.equal(current.sent.length, 0, "old document/origin binding must fail closed");
  current.w[bindKey](token, 2, marker, "https://dapp.test");
  assert.equal(current.sent.length, 1);
  assert.equal(current.sent[0].body.capability, token);
  current.answer({ id: current.sent[0].body.id, capability: oldToken, origin: "https://dapp.test", result: "stale" });
  current.answer({ id: current.sent[0].body.id, capability: token, origin: "https://dapp.test", result: "current" });
  assert.equal(await call, "current");

  // 乱序到达的携带他源 origin 的事件不能被本文档读取；capability 之外的第二道防线。
  const foreign = current.w.nearWallet.getNetwork();
  const foreignRequest = current.sent.at(-1);
  let settled = false;
  foreign.then(() => { settled = true; }, () => { settled = true; });
  current.answer({ id: foreignRequest.body.id, capability: token, origin: "https://evil.test", result: "leak" });
  await Promise.resolve();
  assert.equal(settled, false, "an event with a foreign origin must be rejected");
  current.answer({ id: foreignRequest.body.id, capability: token, origin: "https://dapp.test", result: "own" });
  assert.equal(await foreign, "own");

  // 乱序执行的 Eval 不能覆盖已恢复文档中的较新绑定。
  current.w[bindKey](oldToken, 1, marker, "https://dapp.test");
  const timeoutCall = current.w.nearWallet.getNetwork();
  const timeoutRequest = current.sent.at(-1);
  assert.equal(timeoutRequest.body.capability, token);
  const timer = [...current.timers.values()].at(-1);
  assert.equal(timer.ms, 120000);
  timer.fn();
  await assert.rejects(timeoutCall, /did not answer/);
  assert.equal(timeoutRequest.options.signal.aborted, true, "JS timeout must abort native HTTP work");

  const closing = current.w.nearWallet.getAccounts();
  const closingRequest = current.sent.at(-1);
  current.events.get("pagehide")();
  await assert.rejects(closing, /document was closed/);
  assert.equal(closingRequest.options.signal.aborted, true);
  await assert.rejects(current.w.nearWallet.getAccounts(), /document was closed/);

  // 前进或后退缓存恢复同一文档时，原生 capability 会轮换；
  // 页面旧 token 在 host 端仍然无效。
  const restoredToken = "c".repeat(64);
  current.w[bindKey](restoredToken, 3, marker, "https://dapp.test");
  const disconnect = current.w.nearWallet.disconnect();
  const last = current.sent.at(-1);
  assert.equal(last.body.method, "disconnect");
  assert.equal(last.body.capability, restoredToken);
  current.answer({ id: last.body.id, capability: restoredToken, origin: "https://dapp.test", result: null });
  await disconnect;
})().catch(err => { console.error(err); process.exitCode = 1; });
`

const dappNearConnectFixture = `
const assert = require("node:assert/strict");
const vm = require("node:vm");
const script = require("node:fs").readFileSync(0, "utf8");
const events = new Map(), sent = [];
let answer;
const w = {
  document: {}, location: { origin: "https://dapp.test" }, crypto: require("node:crypto").webcrypto,
  AbortController, TextDecoder, Uint8Array, ArrayBuffer, btoa: value => Buffer.from(value, "binary").toString("base64"),
  CustomEvent: class CustomEvent { constructor(type, options) { this.type = type; this.detail = options.detail; } },
  Event: class Event { constructor(type) { this.type = type; } },
  fetch: (url, options) => { sent.push({ body: JSON.parse(options.body) }); return Promise.resolve({}); },
  setTimeout: () => 1, clearTimeout: () => {},
  addEventListener: (name, fn) => { const list = events.get(name) || []; list.push(fn); events.set(name, list); },
  dispatchEvent: event => { for (const fn of events.get(event.type) || []) fn(event); },
  mygo: { on: (name, fn) => { answer = fn; } },
};
w.window = w; w.top = w;
let discovered;
w.addEventListener("near-wallet-injected", event => { discovered = event.detail; });
vm.runInNewContext(script, w);
assert.equal(discovered, undefined, "discovery waits for the selector handshake");
w.dispatchEvent(new w.Event("near-selector-ready"));
assert.equal(discovered.manifest.id, "lintel");
assert.equal(discovered.manifest.type, "injected");
assert.equal(discovered.manifest.features.signAndSendTransactions, true);
assert.equal(discovered.manifest.features.signMessage, false);
assert.equal(discovered.manifest.features.signTransaction, false);
assert.equal(discovered.manifest.features.signInAndSignMessage, false);
assert.equal(discovered.manifest.features.signInWithFunctionCallKey, false);
assert.equal(discovered.manifest.features.signDelegateActions, false);
assert.equal(discovered.manifest.features.mainnet, true);
assert.equal(discovered.manifest.features.testnet, true);
assert.equal(discovered.manifest.platform.web, "https://github.com/davirain-su/lintel");
assert.equal(discovered.manifest.executor, "");
assert.equal(w.nearWallet.isNearWallet, true, "the legacy bridge remains installed");
const token = "a".repeat(64);
w.__cryptoPanelBindNear(token, 1, w.__cryptoPanelNearDocument, "https://dapp.test");
async function call(method, params, result) {
  const promise = method(params);
  const request = sent.at(-1).body;
  answer({ id: request.id, capability: token, origin: "https://dapp.test", result });
  return { result: await promise, request };
}
async function answerNetwork(networkId) {
  await Promise.resolve();
  const networkRequest = sent.at(-1).body;
  assert.equal(networkRequest.method, "getNetwork");
  answer({ id: networkRequest.id, capability: token, origin: "https://dapp.test", result: { networkId } });
  await Promise.resolve();
}
(async () => {
  const signedIn = await call(discovered.signIn, {}, { accounts: [{ accountId: "alice.testnet", publicKey: "ed25519:key" }] });
  assert.deepEqual(signedIn.result, [{ accountId: "alice.testnet", publicKey: "ed25519:key" }]);
  assert.equal(signedIn.request.method, "connect");
  assert.equal(signedIn.request.params.network, undefined);

  await call(discovered.getAccounts, { network: "testnet" }, []);
  assert.equal(sent.at(-1).body.params.network, "testnet");
  const accountsMismatch = discovered.getAccounts({ network: "mainnet" });
  answer({ id: sent.at(-1).body.id, capability: token, origin: "https://dapp.test", error: "network mainnet does not match selected testnet" });
  await assert.rejects(accountsMismatch, /network mainnet does not match selected testnet/);
  const mismatch = discovered.signAndSendTransaction({ network: "mainnet", receiverId: "bob.near", actions: [] });
  await answerNetwork("testnet");
  await assert.rejects(mismatch, /network mainnet does not match selected testnet/);
  const afterMismatch = sent.at(-1).body;
  assert.equal(afterMismatch.method, "getNetwork");

  const current = discovered.signAndSendTransaction({
    receiverId: "contract.testnet", signerId: "alice.testnet",
    actions: [
      { functionCall: { methodName: "set", args: new Uint8Array([123,125]), gas: 30000000000000n, deposit: 0n } },
      { transfer: { deposit: 1n } },
      { addKey: { publicKey: { toString: () => "ed25519:key" }, accessKey: { nonce: 0n,
        permission: { functionCall: { receiverId: "contract.testnet", allowance: 25n, methodNames: ["set"] } } } } },
    ],
  });
  await answerNetwork("testnet");
  const currentRequest = sent.at(-1).body;
  assert.equal(currentRequest.method, "signAndSendTransaction");
  assert.equal(currentRequest.params.network, "testnet");
  assert.equal(currentRequest.params.from, "alice.testnet");
  assert.deepEqual(currentRequest.params.actions[0], { type: "functionCall", method: "set", args: "{}", gas: "30000000000000", deposit: "0" });
  assert.deepEqual(currentRequest.params.actions[1], { type: "transfer", amount: "1" });
  assert.equal(currentRequest.params.actions[2].type, "addFunctionCallKey");
  assert.equal(currentRequest.params.actions[2].allowance, "25");
  const outcome = { status: { SuccessValue: "" }, transaction: { hash: "hash" }, receipts: [{ receipt_id: "r" }] };
  answer({ id: currentRequest.id, capability: token, origin: "https://dapp.test", result: outcome });
  assert.equal(await current, outcome);

  const legacy = discovered.signAndSendTransaction({ receiverId: "bob.testnet", actions: [
    { type: "Transfer", params: { deposit: "1" } },
    { type: "FunctionCall", params: { methodName: "set", args: { greeting: "hello" }, gas: "1", deposit: "0" } },
    { type: "AddKey", params: { publicKey: "ed25519:key", accessKey: { permission: { functionCall: { receiverId: "contract.testnet", allowance: "1", methodNames: [] } } } } },
  ] });
  await answerNetwork("testnet");
  assert.deepEqual(sent.at(-1).body.params.actions[1], {
    type: "functionCall", method: "set", args: "{\"greeting\":\"hello\"}", gas: "1", deposit: "0",
  });
  assert.deepEqual(sent.at(-1).body.params.actions[2], {
    type: "addFunctionCallKey", publicKey: "ed25519:key", receiverId: "contract.testnet", methodNames: [], allowance: "1",
  });
  answer({ id: sent.at(-1).body.id, capability: token, origin: "https://dapp.test", error: "stop" });
  await assert.rejects(legacy, /stop/);

  const bytes = discovered.signAndSendTransaction({ receiverId: "contract.testnet", actions: [
    { functionCall: { methodName: "raw", args: new Uint8Array([255]), gas: 1n, deposit: 0n } },
  ] });
  await answerNetwork("testnet");
  assert.equal(sent.at(-1).body.params.actions[0].args, "base64:/w==");
  answer({ id: sent.at(-1).body.id, capability: token, origin: "https://dapp.test", result: outcome });
  assert.equal(await bytes, outcome);

  const batch = w.nearWallet.signAndSendTransactions({ transactions: [
    { receiverId: "one.testnet", actions: [{ type: "Transfer", params: { deposit: "1" } }] },
    { receiverId: "two.testnet", actions: [{ type: "Transfer", params: { deposit: "2" } }] },
  ] });
  await Promise.resolve(); await Promise.resolve();
  assert.equal(sent.at(-1).body.method, "getNetwork");
  await answerNetwork("testnet");
  const firstCount = sent.length;
  assert.equal(sent.at(-1).body.params.receiverId, "one.testnet");
  answer({ id: sent.at(-1).body.id, capability: token, origin: "https://dapp.test", result: { transaction: { hash: "one" } } });
  await Promise.resolve(); await Promise.resolve();
  await answerNetwork("testnet");
  assert.equal(sent.at(-1).body.params.receiverId, "two.testnet");
  answer({ id: sent.at(-1).body.id, capability: token, origin: "https://dapp.test", result: { transaction: { hash: "two" } } });
  assert.deepEqual(await batch, [{ transaction: { hash: "one" } }, { transaction: { hash: "two" } }]);

  for (const method of ["signMessage", "verifyOwner", "signTransaction", "signTransactions", "signDelegateActions", "signInAndSignMessage"]) {
    await assert.rejects(discovered[method]({}), new RegExp(method + ".*unsupported"));
  }
  await assert.rejects(discovered.signIn({ addFunctionCallKey: { contractId: "x" } }), /addFunctionCallKey during signIn is unsupported/);
})().catch(err => { console.error(err); process.exitCode = 1; });
`
