import type { TraceEvent } from "../model.ts";
import { payload } from "./events.ts";
export function dimensions(event?: TraceEvent) {
  const p = payload(event);
  return ["saturation", "staleness", "relevance", "coherence"].map(name => ({
    name,
    value: typeof p[name] === "number" && Number.isFinite(p[name]) ? Math.max(0, Math.min(1, p[name])) * 100 : undefined,
    freshness: p.freshness?.[name] || (p[name] == null ? "unavailable" : "unknown"),
  }));
}
