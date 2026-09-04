export type Session = {
  ID: string; ParentID: string; Task: string; Status: string; CreatedAt: string;
  Turn: number; Health?: number; Children?: Session[];
};

export type TraceEvent = {
  id: number; session_id: string; seq: number; turn: number; type: string;
  payload: unknown; visible: boolean; created_at: string; [key: string]: unknown;
};

export type Intervention = { seq: number; action: string; reason: string; resultSeq?: number; affectedSeqs?: number[] };
export type HealthPoint = {
  seq: number; turn: number; composite: number; latencyMS: number;
  scores: Record<string, number | undefined>; intervention?: Intervention;
  lowRelevance: RelevanceChunk[]; contradictions: Contradiction[];
};
export type ToolSpan = { callID: string; name: string; durationMS: number; offsetMS: number; resultSeq?: number };
export type RelevanceChunk = { seq: number; similarity: number };
export type Contradiction = { left_seq: number; right_seq: number; reason?: string };

type Payload = Record<string, unknown>;
const payload = (item: TraceEvent) => item.payload as Payload;

export function deriveDiagnostics(events: TraceEvent[]) {
  const hiddenBy = new Map<number, number>();
  const interventionByTurn = new Map<number, Intervention>();
  const contextEvents: TraceEvent[] = [];
  const callsByTurn = new Map<number, Array<{ callID: string; name: string }>>();
  const starts = new Map<string, number>();
  const results = new Map<string, { durationMS: number; seq: number }>();

  for (const item of events) {
    const value = payload(item);
    if (item.type.startsWith("context.")) contextEvents.push(item);
    if (item.type === "context.visibility") {
      for (const change of (value.changes as Array<{ seq: number; to: boolean }> || [])) if (!change.to) hiddenBy.set(change.seq, item.seq);
    }
    if (item.type === "context.compact") for (const seq of (value.replaced_seqs as number[] || [])) hiddenBy.set(seq, item.seq);
    if (item.type === "intervene.fire") interventionByTurn.set(Number(value.turn_scored), { seq: item.seq, action: String(value.action), reason: String(value.reason), affectedSeqs: value.affected_seqs as number[] | undefined });
    if (item.type === "assistant.tool_call") {
      const values = callsByTurn.get(item.turn) || [];
      values.push({ callID: String(value.call_id), name: String(value.name) });
      callsByTurn.set(item.turn, values);
    }
    if (item.type === "tool.start") starts.set(String(value.call_id), Date.parse(String(value.started_at)));
    if (item.type === "tool.result") results.set(String(value.call_id), { durationMS: Number(value.duration_ms) || 0, seq: item.seq });
  }

  for (const intervention of interventionByTurn.values()) {
    const affected = new Set(intervention.affectedSeqs || []);
    const expectedType: Record<string, string> = { warn: "context.inject", prune: "context.visibility", compact: "context.compact", reanchor: "context.inject" };
    if (!expectedType[intervention.action]) continue;
    const result = contextEvents.findLast(item => item.seq < intervention.seq && item.type === expectedType[intervention.action] && (affected.size === 0 || contextTouches(item, affected)));
    intervention.resultSeq = result?.seq;
  }
  const health = events.filter(item => item.type === "score.health").map(item => {
    const value = payload(item), details = (value.details || {}) as Record<string, Payload>;
    const lowRelevance = ((details.relevance?.lowest as Array<{ seq: number; sim: number }>) || []).map(chunk => ({ seq: chunk.seq, similarity: chunk.sim }));
    const contradictions = (details.coherence?.contradictory_pairs as Contradiction[]) || [];
    return {
      seq: item.seq, turn: Number(value.turn_scored), composite: Number(value.composite), latencyMS: Number(value.latency_ms) || 0,
      scores: { saturation: optionalNumber(value.saturation), staleness: optionalNumber(value.staleness), relevance: optionalNumber(value.relevance), coherence: optionalNumber(value.coherence) },
      intervention: interventionByTurn.get(Number(value.turn_scored)),
      lowRelevance, contradictions,
    } satisfies HealthPoint;
  });
  const tools = new Map<number, ToolSpan[]>();
  for (const [turn, calls] of callsByTurn) {
    const validStarts = calls.map(call => starts.get(call.callID)).filter((value): value is number => value !== undefined);
    const origin = validStarts.length ? Math.min(...validStarts) : 0;
    tools.set(turn, calls.map(call => ({ callID: call.callID, name: call.name, durationMS: results.get(call.callID)?.durationMS || 0, offsetMS: starts.has(call.callID) ? starts.get(call.callID)! - origin : 0, resultSeq: results.get(call.callID)?.seq })));
  }
  return { health, tools, hiddenBy };
}

function optionalNumber(value: unknown) { return typeof value === "number" ? value : undefined; }
function contextTouches(item: TraceEvent, affected: Set<number>) {
  const value = payload(item);
  const seqs = item.type === "context.visibility" ? (value.changes as Array<{ seq: number }> || []).map(change => change.seq) : (value.replaced_seqs as number[] || []);
  return seqs.some(seq => affected.has(seq));
}
