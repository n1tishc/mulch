import type { TraceEvent } from "../model.ts";
export type Payload = Record<string, any>;
export const payload = (e?: TraceEvent): Payload => e?.payload && typeof e.payload === "object" ? e.payload as Payload : {};

export class EventBuffer {
  private events = new Map<number, TraceEvent>();
  cursor = 0;
  readonly sessionID: string;
  constructor(sessionID: string) { this.sessionID=sessionID; }
  add(event: TraceEvent) {
    if (event.session_id !== this.sessionID || !Number.isSafeInteger(event.seq) || event.seq < 1 || typeof event.type !== "string") throw new Error("Invalid session event received");
    this.events.set(event.seq, event);
    while (this.events.has(this.cursor + 1)) this.cursor++;
  }
  snapshot() { return [...this.events.values()].sort((a,b) => a.seq-b.seq); }
}

export type ConversationRow = { key: string; kind: "user" | "assistant" | "tool" | "repair" | "end"; event: TraceEvent; text: string; result?: TraceEvent };
export function conversation(events: TraceEvent[]): ConversationRow[] {
  const rows: ConversationRow[] = [];
  const assistants = new Map<number, { row: ConversationRow; deltas: string[]; finals: string[] }>();
  const tools = new Map<string, ConversationRow>();
  for (const event of events) {
    const p = payload(event);
    if (event.type === "user.message") rows.push({ key: `user-${event.seq}`, kind: "user", event, text: String(p.text || "") });
    if (event.type === "assistant.delta" || event.type === "assistant.message") {
      let item = assistants.get(event.turn);
      if (!item) { const row: ConversationRow = { key: `assistant-${event.turn}`, kind: "assistant", event, text: "" }; item = { row, deltas: [], finals: [] }; assistants.set(event.turn,item); rows.push(row); }
      if (event.type === "assistant.delta") item.deltas.push(String(p.text || "")); else { item.finals.push(String(p.text || "")); item.row.event = event; }
    }
    if (event.type === "assistant.tool_call") {
      const row: ConversationRow = { key: `tool-${event.seq}`, kind: "tool", event, text: String(p.name || "Tool") }; tools.set(`${event.turn}:${p.call_id}`,row); rows.push(row);
    }
    if (event.type === "tool.result") { const row = tools.get(`${event.turn}:${p.call_id}`); if (row) row.result = event; }
    if (event.type === "intervene.fire") rows.push({ key: `repair-${event.seq}`, kind: "repair", event, text: `${p.action}: ${p.reason}` });
    if (event.type === "session.end") rows.push({ key: `end-${event.seq}`, kind: "end", event, text: String(p.status || "finished") });
  }
  for (const {row,deltas,finals} of assistants.values()) row.text = (finals.length ? finals : deltas).join("");
  return rows;
}

export function recordedConfig(events: TraceEvent[], at = Infinity) {
  return events.findLast(e => e.type === "run.config" && e.seq <= at);
}

export function requestContext(events: TraceEvent[], request: TraceEvent) {
  const ids = payload(request).visible_event_seqs as number[] | undefined;
  const bySeq = new Map(events.map(e => [e.seq,e]));
  return (ids || []).map(seq => ({seq,event:bySeq.get(seq)}));
}

export function contextChanges(events: TraceEvent[], mutation: TraceEvent) {
  const p = payload(mutation), bySeq = new Map(events.map(e => [e.seq,e]));
  if (mutation.type === "context.visibility") return (p.changes || []).map((c: {seq:number;from:boolean;to:boolean}) => ({seq:c.seq, before:c.from ? "visible" : "hidden", after:c.to ? "visible" : "hidden", event:bySeq.get(c.seq)}));
  if (mutation.type === "context.compact") return [...(p.replaced_seqs || []).map((seq:number) => ({seq,before:"visible",after:"replaced",event:bySeq.get(seq)})), {seq:mutation.seq,before:"absent",after:"summary inserted",event:mutation}];
  if (mutation.type === "context.inject") return [{seq:mutation.seq,before:"absent",after:"inserted",event:mutation}];
  return [];
}

export function eventText(e?: TraceEvent): string {
  if (!e) return "Original event is unavailable.";
  const p = payload(e);
  return String(p.text ?? p.output ?? p.summary ?? JSON.stringify(p, null, 2));
}
