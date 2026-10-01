import test from "node:test";
import assert from "node:assert/strict";
import { createStateManager } from "../../assets/web/state_manager.js";
import { computeLCSDiff } from "../../assets/web/diff.js";
import { createSocketHandler } from "../../assets/web/socket_handler.js";
import { ignoredEvtTypes } from "../../assets/web/compare_window.js";
import {
  getSipMessageInfo,
  analyzeSipEvents,
  generateSipLadderHTML,
} from "../../assets/web/sip_renderer.js";

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

test("SIP ladder parser extracts CSeq, Call-ID, method and status", () => {
  const invite = [
    "# 12:00:01.000 | TX",
    "SIP 10.0.0.1:5060 -> 10.0.0.2:5060",
    "INVITE sip:alice@example.com SIP/2.0",
    "Call-ID: test-call-id-1",
    "CSeq: 42 INVITE",
  ].join("\n");
  const info = getSipMessageInfo(invite);
  assert.equal(info.method, "INVITE");
  assert.equal(info.cseqNumber, 42);
  assert.equal(info.cseqMethod, "INVITE");
  assert.equal(info.callId, "test-call-id-1");
  assert.equal(info.isResponse, false);

  const ok = [
    "# 12:00:01.050 | RX",
    "SIP 10.0.0.2:5060 -> 10.0.0.1:5060",
    "SIP/2.0 200 OK",
    "Call-ID: test-call-id-1",
    "CSeq: 42 INVITE",
  ].join("\n");
  const okInfo = getSipMessageInfo(ok);
  assert.equal(okInfo.isResponse, true);
  assert.equal(okInfo.statusCode, 200);
  assert.equal(okInfo.statusText, "OK");
  assert.equal(okInfo.cseqNumber, 42);
  assert.equal(okInfo.cseqMethod, "INVITE");
});

test("analyzeSipEvents detects missing ACKs and counts retransmissions", () => {
  const evtInvite = {
    type: "SIP",
    param: "# 12:00:01.000 | TX\nSIP 10.0.0.1:5060 -> 10.0.0.2:5060\nINVITE sip:alice@example.com SIP/2.0\nCall-ID: c1\nCSeq: 100 INVITE",
  };
  const evt200Ok1 = {
    type: "SIP",
    param: "# 12:00:01.050 | RX\nSIP 10.0.0.2:5060 -> 10.0.0.1:5060\nSIP/2.0 200 OK\nCall-ID: c1\nCSeq: 100 INVITE",
  };
  const evt200OkRetransmit = {
    type: "SIP",
    param: "# 12:00:01.550 | RX\nSIP 10.0.0.2:5060 -> 10.0.0.1:5060\nSIP/2.0 200 OK\nCall-ID: c1\nCSeq: 100 INVITE",
  };

  // Without ACK: 200 OK has missingAck = true, retransmission has retransmitCount = 1
  const analysisNoAck = analyzeSipEvents([evtInvite, evt200Ok1, evt200OkRetransmit]);
  assert.equal(analysisNoAck[0].cseqNumber, 100);
  assert.equal(analysisNoAck[0].missingAck, false);
  assert.equal(analysisNoAck[1].cseqNumber, 100);
  assert.equal(analysisNoAck[1].missingAck, true);
  assert.equal(analysisNoAck[1].retransmitCount, 0);
  assert.equal(analysisNoAck[2].missingAck, true);
  assert.equal(analysisNoAck[2].retransmitCount, 1);

  // With matching ACK: 200 OK has missingAck = false
  const evtAck = {
    type: "SIP",
    param: "# 12:00:01.060 | TX\nSIP 10.0.0.1:5060 -> 10.0.0.2:5060\nACK sip:alice@example.com SIP/2.0\nCall-ID: c1\nCSeq: 100 ACK",
  };
  const analysisWithAck = analyzeSipEvents([evtInvite, evt200Ok1, evtAck]);
  assert.equal(analysisWithAck[1].missingAck, false);

  // Non-INVITE (e.g. BYE 200 OK) should not expect an ACK
  const evtByeOk = {
    type: "SIP",
    param: "# 12:00:02.000 | RX\nSIP 10.0.0.2:5060 -> 10.0.0.1:5060\nSIP/2.0 200 OK\nCall-ID: c1\nCSeq: 101 BYE",
  };
  const analysisBye = analyzeSipEvents([evtByeOk]);
  assert.equal(analysisBye[0].missingAck, false);
});

test("generateSipLadderHTML renders CSeq badge, retransmit count, and missing ACK pill", () => {
  const evt = {
    type: "SIP",
    param: "# 12:00:01.050 | RX\nSIP 10.0.0.2:5060 -> 10.0.0.1:5060\nSIP/2.0 200 OK\nCall-ID: c1\nCSeq: 100 INVITE",
  };
  const html = generateSipLadderHTML(evt, 5, {
    cseqNumber: 100,
    retransmitCount: 2,
    missingAck: true,
  });
  assert.match(html, /sip-cseq-badge/);
  assert.match(html, /CSeq 100/);
  assert.match(html, /sip-badge-retransmit/);
  assert.match(html, /🔄 Retransmit #2/);
  assert.match(html, /sip-badge-missing-ack/);
  assert.match(html, /⚠️ Missing ACK/);
  assert.match(html, /has-missing-ack/);
  assert.match(html, /is-retransmit/);
});

