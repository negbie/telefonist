import test from "node:test";
import assert from "node:assert/strict";
import { createStateManager } from "../../assets/web/state_manager.js";
import { computeLCSDiff } from "../../assets/web/diff.js";
import { createSocketHandler } from "../../assets/web/socket_handler.js";
import { ignoredEvtTypes } from "../../assets/web/compare_window.js";

test("refresh preserves unsaved drafts while pruning deleted clean files", () => {
  const state = createStateManager();
  state.updateCache("lab:call", { name: "call", project: "lab", original: "dial alice" });
  state.updateCache("lab:call", { current: "dial bob" });
  state.updateCache("lab:deleted", { name: "deleted", project: "lab", original: "hangup" });
  state.pruneStale([]);
  assert.equal(state.getEntry("lab:call").current, "dial bob");
  assert.equal(state.isDirty("lab:call"), true);
  assert.equal(state.getEntry("lab:deleted"), undefined);
  state.updateCache("lab:call", { original: "dial bob" });
  assert.equal(state.isDirty("lab:call"), false);
});

test("diff retains repeated SIP events and reconstructs both runs", () => {
  const a = ["INVITE", "180", "200", "ACK", "BYE", "200"];
  const b = ["INVITE", "100", "180", "180", "200", "ACK", "BYE", "200"];
  const items = (values) => values.map(value => ({ compare: value, display: value }));
  const diff = computeLCSDiff(items(a), items(b));
  assert.deepEqual(diff.filter(d => d.type !== "b").map(d => d.itemA.compare), a);
  assert.deepEqual(diff.filter(d => d.type !== "a").map(d => d.itemB.compare), b);
  assert.deepEqual(diff.filter(d => d.type === "b").map(d => d.itemB.compare), ["100", "180"]);
  assert.equal(diff.filter(d => d.type === "a").length, 0);
});

test("WebSocket replay batches and live arrivals preserve order across frame budgets", (t) => {
  const frames = [];
  let connection;
  class FakeSocket {
    static OPEN = 1;
    readyState = FakeSocket.OPEN;
    constructor() { connection = this; }
  }
  const originals = new Map(["window", "WebSocket", "requestAnimationFrame"].map(k => [k, Object.getOwnPropertyDescriptor(globalThis, k)]));
  t.after(() => {
    for (const [key, descriptor] of originals) {
      if (descriptor) Object.defineProperty(globalThis, key, descriptor);
      else delete globalThis[key];
    }
  });
  globalThis.window = { WebSocket: FakeSocket };
  globalThis.WebSocket = FakeSocket;
  globalThis.requestAnimationFrame = callback => frames.push(callback);
  const received = [];
  createSocketHandler("ws://example.test/ws", { onMessage: event => received.push(event.sequence) });
  const encode = sequence => JSON.stringify({ event: true, type: "LOG", sequence });
  connection.onmessage({ data: Array.from({ length: 600 }, (_, i) => encode(i)).join("\n") });
  assert.equal(received.length, 0);
  frames.shift()();
  assert.equal(received.length, 250);
  connection.onmessage({ data: encode(600) });
  while (frames.length) frames.shift()();
  assert.deepEqual(received, Array.from({ length: 601 }, (_, i) => i));
});

test("large comparison remains bounded and preserves all evidence", () => {
  const a=Array.from({length:3000},(_,i)=>({compare:`a${i}`,display:`a${i}`}));
  const b=Array.from({length:3000},(_,i)=>({compare:`b${i}`,display:`b${i}`}));
  const diff=computeLCSDiff(a,b);
  assert.deepEqual(diff.filter(x=>x.itemA).map(x=>x.itemA),a);
  assert.deepEqual(diff.filter(x=>x.itemB).map(x=>x.itemB),b);
});

test("draft survives refresh and is removed from storage after saving", t => {
  const previous=Object.getOwnPropertyDescriptor(globalThis,"sessionStorage");
  t.after(()=>{if(previous)Object.defineProperty(globalThis,"sessionStorage",previous);else delete globalThis.sessionStorage;});
  const storage=new Map();globalThis.sessionStorage={getItem:k=>storage.get(k),setItem:(k,v)=>storage.set(k,v)};
  let state=createStateManager();
  state.updateCache("lab:call",{name:"call",project:"lab",original:"dial alice",current:"dial bob",revision:"v1"});
  state=createStateManager();
  assert.equal(state.getEntry("lab:call").current,"dial bob");
  assert.equal(state.getEntry("lab:call").revision,"v1");
  state.updateCache("lab:call",{original:"dial bob",revision:"v2"});
  assert.equal(createStateManager().getEntry("lab:call"),undefined);
});

test("flow compare view excludes internal and lifecycle event types (MODULE, RESPONSE, NATIVE_DONE)", () => {
  assert.equal(ignoredEvtTypes.has("module"), true);
  assert.equal(ignoredEvtTypes.has("response"), true);
  assert.equal(ignoredEvtTypes.has("native_done"), true);
  assert.equal(ignoredEvtTypes.has("call_established"), false);
  assert.equal(ignoredEvtTypes.has("call_closed"), false);
  assert.equal(ignoredEvtTypes.has("audio_report"), false);
});
