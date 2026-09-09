import type { TraceEvent } from "../model.ts";
import { payload } from "./events.ts";
export type TraceNode = { event: TraceEvent; label: string; children: TraceNode[] };
export type TraceTurn = { turn: number; nodes: TraceNode[]; deltas: number };
export function traceTree(events: TraceEvent[]): TraceTurn[] {
  const turns = new Map<number, TraceTurn>(), calls = new Map<string, TraceNode>();
  for (const event of events) {
    let group = turns.get(event.turn);
    if (!group) { group = {turn:event.turn,nodes:[],deltas:0}; turns.set(event.turn,group); }
    if (event.type === "assistant.delta") { group.deltas++; continue; }
    const p = payload(event);
    const label = event.type === "assistant.tool_call" ? `Tool · ${p.name || "unknown"}`
      : event.type === "tool.result" ? `${p.cancelled ? "Cancelled" : p.is_error || p.timed_out ? "Failed" : "Finished"} · ${p.duration_ms ?? "?"} ms`
      : event.type === "score.health" ? `Health · ${Number(p.composite).toFixed(1)} · scored turn ${p.turn_scored}`
      : event.type === "score.partial" ? `Scorer unavailable · ${p.name} · ${p.reason || "unknown"}`
      : event.type === "intervene.fire" ? `Repair · ${p.action}`
      : event.type === "intervene.skip" ? `Repair skipped · ${p.reason || "reason unrecorded"}`
      : event.type === "llm.request" ? `Model request · ${p.message_count ?? "?"} messages`
      : event.type === "session.end" ? `Task ${p.status || "ended"}`
      : event.type.replaceAll(".", " · ").replaceAll("_", " ");
    const node: TraceNode = {event,label,children:[]}, key = `${event.turn}:${p.call_id}`;
    if ((event.type === "tool.start" || event.type === "tool.result") && calls.has(key)) calls.get(key)!.children.push(node);
    else group.nodes.push(node);
    if (event.type === "assistant.tool_call") calls.set(key,node);
  }
  return [...turns.values()];
}
