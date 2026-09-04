import { FormEvent, useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { deriveDiagnostics, type HealthPoint, type Session, type ToolSpan, type TraceEvent } from "./model.ts";
import baseStyles from "./viewer.module.css";
import diagnosticStyles from "./diagnostics.module.css";

const styles = { ...baseStyles, ...diagnosticStyles };

async function request(path: string, init?: RequestInit) { const response = await fetch(path, { ...init, headers: { "Content-Type": "application/json", ...init?.headers } }); if (!response.ok) throw new Error(await response.text()); return response.json(); }
function findSession(items: Session[], id: string): Session | undefined { for (const item of items) { if (item.ID === id) return item; const child = findSession(item.Children || [], id); if (child) return child; } }
type Diagnostics = ReturnType<typeof deriveDiagnostics>;

function Viewer() {
  const [sessions, setSessions] = useState<Session[]>([]), [selected, setSelected] = useState(new URLSearchParams(location.search).get("session") || ""), [events, setEvents] = useState<TraceEvent[]>([]), [detail, setDetail] = useState<TraceEvent | null>(null), [notice, setNotice] = useState("");
  const lastSeq = useRef(0), ended = useRef(false);
  const refresh = () => fetch("/api/sessions").then(r => r.json()).then((items: Session[]) => { setSessions(items); setSelected(x => x || items[0]?.ID || ""); });
  useEffect(() => { void refresh(); const timer = window.setInterval(() => void refresh(), 1500); return () => clearInterval(timer); }, []);
  useEffect(() => {
    if (!selected) return;
    let disposed = false, socket: WebSocket | undefined, retry: number | undefined;
    setEvents([]); setDetail(null); lastSeq.current = 0; ended.current = false; history.replaceState(null, "", `?session=${encodeURIComponent(selected)}`);
    const connect = () => {
      socket = new WebSocket(`${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/ws/sessions/${selected}?from=${lastSeq.current + 1}`);
      socket.onopen = () => setNotice("Live connection active");
      socket.onmessage = message => { const item = JSON.parse(message.data) as TraceEvent; lastSeq.current = Math.max(lastSeq.current, item.seq); setEvents(current => current.some(x => x.seq === item.seq) ? current : [...current, item].sort((a, b) => a.seq - b.seq)); if (item.type === "session.end") { ended.current = true; setNotice("Session finished"); void refresh(); } };
      socket.onclose = () => { if (!disposed && !ended.current) { setNotice("Reconnecting…"); retry = window.setTimeout(connect, 750); } };
    };
    connect(); return () => { disposed = true; if (retry) clearTimeout(retry); socket?.close(); };
  }, [selected]);
  const turns = useMemo(() => events.reduce((grouped, item) => { const values = grouped.get(item.turn) || []; values.push(item); grouped.set(item.turn, values); return grouped; }, new Map<number, TraceEvent[]>()), [events]);
  const diagnostics = useMemo(() => deriveDiagnostics(events), [events]);
  const act = async (operation: () => Promise<unknown>, success: string) => { try { setNotice("Working…"); await operation(); setNotice(success); await refresh(); } catch (error) { setNotice(error instanceof Error ? error.message : String(error)); } };
  const start = (task: string) => act(async () => { const result = await request("/api/sessions", { method: "POST", body: JSON.stringify({ task, opts: {} }) }); setSelected(result.id); }, "Session started");
  const steer = (text: string) => act(() => request(`/api/sessions/${selected}/steer`, { method: "POST", body: JSON.stringify({ text }) }), "Steer queued for the next turn");
  const branch = (at: number) => act(async () => { const result = await request(`/api/sessions/${selected}/branch`, { method: "POST", body: JSON.stringify({ at }) }); setSelected(result.id); }, `Branched from #${at}`);
  const selectEvent = (item: TraceEvent) => { setDetail(item); history.replaceState(null, "", `?session=${encodeURIComponent(selected)}&event=${item.seq}`); };
  const current = findSession(sessions, selected);
  return <main className={styles.shell} aria-busy={notice === "Working…"}><SessionList sessions={sessions} selected={selected} onSelect={setSelected} onStart={start} /><Timeline task={current?.Task} status={current?.Status} sessionID={selected} turns={turns} diagnostics={diagnostics} onSelect={selectEvent} onSteer={steer} onCancel={() => act(() => request(`/api/sessions/${selected}`, { method: "DELETE" }), "Cancellation requested")} onBranch={branch} /><DetailPane event={detail} events={events} diagnostics={diagnostics} onSelect={selectEvent} /><div className={styles.notice} aria-live="polite">{notice}</div></main>;
}

function SessionList({ sessions, selected, onSelect, onStart }: { sessions: Session[]; selected: string; onSelect: (id: string) => void; onStart: (task: string) => void }) {
  const [task, setTask] = useState("");
  const row = (item: Session, depth = 0): React.ReactNode => <div key={item.ID} className={styles.sessionGroup}><button style={{ paddingLeft: 12 + depth * 16 }} className={selected === item.ID ? styles.active : ""} onClick={() => onSelect(item.ID)}><span>{depth > 0 && <b aria-hidden="true">↳ </b>}{item.Task}</span><small><i className={styles[item.Status]} />{item.Status} · T{item.Turn}{item.Health != null && ` · ${Math.round(item.Health)}`}</small></button>{item.Children?.map(child => row(child, depth + 1))}</div>;
  return <aside><header><span className={styles.mark}>M</span><div><h1>mulch</h1><small>durable traces</small></div></header><form className={styles.start} onSubmit={(e: FormEvent) => { e.preventDefault(); if (task.trim()) { onStart(task.trim()); setTask(""); } }}><label htmlFor="new-task">New session</label><textarea id="new-task" value={task} onChange={e => setTask(e.target.value)} placeholder="Describe the task…" /><button disabled={!task.trim()}>Start session</button></form><nav aria-label="Sessions">{sessions.map(item => row(item))}</nav></aside>;
}

function Timeline({ task, status, sessionID, turns, diagnostics, onSelect, onSteer, onCancel, onBranch }: { task?: string; status?: string; sessionID: string; turns: Map<number, TraceEvent[]>; diagnostics: Diagnostics; onSelect: (event: TraceEvent) => void; onSteer: (text: string) => void; onCancel: () => void; onBranch: (at: number) => void }) {
  const [steer, setSteer] = useState(""), running = status === "running", allEvents = [...turns.values()].flat();
  const selectSeq = (seq?: number) => { const item = allEvents.find(x => x.seq === seq); if (item) onSelect(item); };
  return <section className={styles.timeline}><div className={styles.heading}><div><small>SESSION</small><h2>{task || "No sessions"}</h2></div><code>{sessionID.slice(0, 12)}</code></div>{sessionID && <><div className={styles.controls}><label htmlFor="steer">Steer next turn</label><input id="steer" value={steer} disabled={!running} onChange={e => setSteer(e.target.value)} placeholder={running ? "Add direction…" : "Session is not running"} /><button disabled={!running || !steer.trim()} onClick={() => { onSteer(steer.trim()); setSteer(""); }}>Queue steer</button><button className={styles.danger} disabled={!running} onClick={onCancel}>Cancel</button></div><HealthStrip points={diagnostics.health} onSelect={selectSeq} /></>}{[...turns].filter(([turn]) => turn > 0).map(([turn, items]) => <article key={turn}><div><h3>Turn {turn}</h3><button className={styles.branch} onClick={() => onBranch(items[items.length - 1].seq)}>Branch here</button></div><div><ToolLanes tools={diagnostics.tools.get(turn) || []} onSelect={selectSeq} /><div className={styles.cards}>{items.filter(item => item.type !== "tool.start").map(item => { const steered = item.type === "user.message" && (item.payload as { origin?: string })?.origin === "steer", cause = diagnostics.hiddenBy.get(item.seq); return <button id={`event-${item.seq}`} key={item.seq} className={`${!item.visible || cause ? styles.dimmed : ""} ${steered ? styles.steered : ""}`} onClick={() => onSelect(item)} title={cause ? `Hidden or replaced by event #${cause}` : undefined}><small>#{item.seq}{steered && <b>STEER</b>}</small><strong>{item.type}</strong><time>{new Date(item.created_at).toLocaleTimeString()}</time>{cause && <em>hidden by #{cause}</em>}</button>; })}</div></div></article>)}</section>;
}

const thresholds = [{ value: 75, label: "warn" }, { value: 60, label: "prune" }, { value: 45, label: "compact" }, { value: 35, label: "reanchor" }, { value: 20, label: "escalate" }];
function HealthStrip({ points, onSelect }: { points: HealthPoint[]; onSelect: (seq: number) => void }) {
  if (!points.length) return <section className={styles.health}><strong>Context health</strong><small>Waiting for a scored turn</small></section>;
  const x = (index: number) => points.length === 1 ? 50 : 4 + index * 92 / (points.length - 1), y = (score: number) => 100 - Math.max(0, Math.min(100, score));
  return <section className={styles.health} aria-label="Context health by scored turn"><div className={styles.healthTitle}><div><strong>Context health</strong><small>Composite score · select a point for details</small></div><b>{Math.round(points.at(-1)!.composite)}</b></div><svg viewBox="0 0 100 100" role="img" aria-label="Composite health line with intervention thresholds">{thresholds.map(line => <g key={line.value}><line x1="0" x2="100" y1={y(line.value)} y2={y(line.value)} className={styles.threshold} /><text x="99" y={y(line.value) - 1}>{line.label}</text></g>)}<polyline points={points.map((point, index) => `${x(index)},${y(point.composite)}`).join(" ")} className={styles.healthLine} />{points.map((point, index) => <g key={point.seq} className={styles.healthPoint} tabIndex={0} role="button" aria-label={healthTitle(point)} onClick={() => onSelect(point.seq)} onKeyDown={e => { if (e.key === "Enter" || e.key === " ") onSelect(point.seq); }}><circle cx={x(index)} cy={y(point.composite)} r="2.7" /><title>{healthTitle(point)}</title>{point.intervention && <path d={`M ${x(index) - 2} ${y(point.composite) - 7} L ${x(index) + 2} ${y(point.composite) - 7} L ${x(index)} ${y(point.composite) - 3} Z`} className={styles.intervention} />}</g>)}</svg><div className={styles.healthLegend}><span>— composite</span><span>▲ intervention</span><span>thresholds 75 / 60 / 45 / 35 / 20</span></div></section>;
}
function healthTitle(point: HealthPoint) { const scores = Object.entries(point.scores).filter(([, value]) => value != null).map(([name, value]) => `${name} ${Math.round(value!)}`).join(", "); return `Turn ${point.turn}: health ${point.composite.toFixed(1)}; ${scores}; ${point.latencyMS}ms${point.intervention ? `; ${point.intervention.action}: ${point.intervention.reason}` : ""}`; }
function ToolLanes({ tools, onSelect }: { tools: ToolSpan[]; onSelect: (seq?: number) => void }) { if (!tools.length) return null; const extent = Math.max(...tools.map(tool => tool.offsetMS + tool.durationMS), 1); return <section className={styles.toolLanes} aria-label="Parallel tool calls"><small>TOOL CONCURRENCY · MODEL ORDER</small>{tools.map((tool, index) => <button key={tool.callID} onClick={() => onSelect(tool.resultSeq)}><b>{index + 1}. {tool.name}</b><span><i style={{ marginLeft: `${tool.offsetMS / extent * 100}%`, width: `${Math.max(tool.durationMS / extent * 100, 2)}%` }} /></span><em>{tool.durationMS}ms</em></button>)}</section>; }

function DetailPane({ event, events, diagnostics, onSelect }: { event: TraceEvent | null; events: TraceEvent[]; diagnostics: Diagnostics; onSelect: (event: TraceEvent) => void }) {
  const health = event?.type === "score.health" ? diagnostics.health.find(point => point.seq === event.seq) : undefined;
  const intervention = event?.type === "intervene.fire" ? diagnostics.health.find(point => point.intervention?.seq === event.seq)?.intervention : undefined;
  const link = (seq: number, label: string) => { const target = events.find(item => item.seq === seq); return target && <button className={styles.eventLink} onClick={() => onSelect(target)}>{label} #{seq}</button>; };
  return <aside className={styles.detail}><div className={styles.heading}><div><small>EVENT DETAIL</small><h2>{event?.type || "Select an event"}</h2></div></div>{health && <section className={styles.scoreDetail}><h3>Health at turn {health.turn}</h3><b>{health.composite.toFixed(1)}</b>{Object.entries(health.scores).filter(([, score]) => score != null).map(([name, score]) => <label key={name}><span>{name} <small>{score!.toFixed(1)}</small></span><meter min="0" max="100" value={score} /></label>)}<p>Scored in {health.latencyMS}ms</p>{health.lowRelevance.length > 0 && <><h4>Low-relevance chunks</h4>{health.lowRelevance.map(item => <div key={item.seq}>{link(item.seq, "Event")} <span>similarity {item.similarity.toFixed(2)}</span></div>)}</>}{health.contradictions.length > 0 && <><h4>Contradictory pairs</h4>{health.contradictions.map(item => <div key={`${item.left_seq}-${item.right_seq}`}>{link(item.left_seq, "Event")} ↔ {link(item.right_seq, "event")}<p>{item.reason}</p></div>)}</>}</section>}{intervention && <section className={styles.scoreDetail}><h3>{intervention.action} intervention</h3><p>{intervention.reason}</p>{intervention.resultSeq && link(intervention.resultSeq, "Resulting context event")}</section>}{event && <pre>{JSON.stringify(event, null, 2)}</pre>}</aside>;
}
createRoot(document.getElementById("root")!).render(<Viewer />);
