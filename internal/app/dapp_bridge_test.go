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
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable for executing the preload fixture")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "-e", dappPreloadFixture)
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
