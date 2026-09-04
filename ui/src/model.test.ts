import assert from "node:assert/strict";
import test from "node:test";
import { deriveDiagnostics, type TraceEvent } from "./model.ts";

const event = (seq: number, turn: number, type: string, payload: unknown, visible = true): TraceEvent => ({
  id: seq, session_id: "s", seq, turn, type, payload, visible, created_at: `2026-09-04T00:00:0${Math.min(seq, 9)}Z`,
});

test("derives health points, intervention links, and diagnostic detail", () => {
  const diagnostics = deriveDiagnostics([
    event(1, 2, "score.health", { turn_scored: 2, composite: 58, saturation: 70, relevance: 31, coherence: 44, latency_ms: 27, details: { relevance: { lowest: [{ seq: 8, sim: .12 }] }, coherence: { contradictory_pairs: [{ left_seq: 3, right_seq: 8, reason: "ports differ" }] } } }),
    event(2, 0, "context.visibility", { changes: [{ seq: 8, from: true, to: false }], by: "ladder", reason: "low relevance" }, false),
    event(3, 3, "intervene.fire", { action: "prune", reason: "low relevance", turn_scored: 2, applied_before_turn: 3, affected_seqs: [8] }),
    event(8, 1, "tool.result", { call_id: "old", duration_ms: 5 }, false),
  ]);
  assert.deepEqual(diagnostics.health.map(point => [point.turn, point.composite, point.intervention?.action, point.intervention?.resultSeq]), [[2, 58, "prune", 2]]);
  assert.equal(diagnostics.hiddenBy.get(8), 2);
  assert.deepEqual(diagnostics.health[0].lowRelevance, [{ seq: 8, similarity: .12 }]);
  assert.deepEqual(diagnostics.health[0].contradictions, [{ left_seq: 3, right_seq: 8, reason: "ports differ" }]);
});

test("keeps diagnostic evidence attached to its scored health point", () => {
  const diagnostics = deriveDiagnostics([
    event(1, 1, "score.health", { turn_scored: 1, composite: 50, details: { relevance: { lowest: [{ seq: 4, sim: .2 }] } } }),
    event(2, 2, "score.health", { turn_scored: 2, composite: 90, details: { relevance: { lowest: [] } } }),
  ]);
  assert.deepEqual(diagnostics.health[0].lowRelevance, [{ seq: 4, similarity: .2 }]);
  assert.deepEqual(diagnostics.health[1].lowRelevance, []);
});

test("does not invent a resulting context event for escalation", () => {
  const diagnostics = deriveDiagnostics([
    event(1, 1, "context.inject", { reason: "older warning" }),
    event(2, 2, "score.health", { turn_scored: 2, composite: 10 }),
    event(3, 3, "intervene.fire", { action: "escalate", reason: "critical", turn_scored: 2 }),
  ]);
  assert.equal(diagnostics.health[0].intervention?.resultSeq, undefined);
});

test("retains model-call order while deriving parallel tool durations", () => {
  const diagnostics = deriveDiagnostics([
    event(1, 1, "assistant.tool_call", { call_id: "slow", name: "bash" }),
    event(2, 1, "assistant.tool_call", { call_id: "fast", name: "read" }),
    event(3, 1, "tool.start", { call_id: "fast", name: "read", started_at: "2026-09-04T00:00:01.100Z" }, false),
    event(4, 1, "tool.start", { call_id: "slow", name: "bash", started_at: "2026-09-04T00:00:01.000Z" }, false),
    event(5, 1, "tool.result", { call_id: "fast", name: "read", duration_ms: 50 }),
    event(6, 1, "tool.result", { call_id: "slow", name: "bash", duration_ms: 300 }),
    event(7, 2, "context.compact", { replaced_seqs: [1, 2], summary: "kept facts" }),
  ]);
  assert.deepEqual(diagnostics.tools.get(1)?.map(tool => [tool.callID, tool.durationMS, tool.offsetMS]), [["slow", 300, 0], ["fast", 50, 100]]);
  assert.equal(diagnostics.hiddenBy.get(1), 7);
  assert.equal(diagnostics.hiddenBy.get(2), 7);
});
