import { FormEvent, useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import styles from "./viewer.module.css";

type Session = { ID: string; ParentID: string; Task: string; Status: string; CreatedAt: string };
type TraceEvent = { id: number; session_id: string; seq: number; turn: number; type: string; payload: unknown; visible: boolean; created_at: string; [key: string]: unknown };

async function request(path: string, init?: RequestInit) {
  const response = await fetch(path, { ...init, headers: { "Content-Type": "application/json", ...init?.headers } });
  if (!response.ok) throw new Error(await response.text());
  return response.json();
}

function Viewer() {
  const [sessions, setSessions] = useState<Session[]>([]), [selected, setSelected] = useState(new URLSearchParams(location.search).get("session") || ""), [events, setEvents] = useState<TraceEvent[]>([]), [detail, setDetail] = useState<TraceEvent | null>(null), [notice, setNotice] = useState("");
  const lastSeq = useRef(0), ended = useRef(false);
  const refresh = () => fetch("/api/sessions").then(r => r.json()).then((items: Session[]) => { setSessions(items); setSelected(x => x || items[0]?.ID || ""); });
  useEffect(() => { void refresh(); }, []);
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
  const act = async (operation: () => Promise<unknown>, success: string) => { try { setNotice("Working…"); await operation(); setNotice(success); await refresh(); } catch (error) { setNotice(error instanceof Error ? error.message : String(error)); } };
  const start = (task: string) => act(async () => { const result = await request("/api/sessions", { method: "POST", body: JSON.stringify({ task, opts: {} }) }); setSelected(result.id); }, "Session started");
  const steer = (text: string) => act(() => request(`/api/sessions/${selected}/steer`, { method: "POST", body: JSON.stringify({ text }) }), "Steer queued for the next turn");
  const branch = (at: number) => act(async () => { const result = await request(`/api/sessions/${selected}/branch`, { method: "POST", body: JSON.stringify({ at }) }); setSelected(result.id); }, `Branched from #${at}`);
  const cancel = () => act(() => request(`/api/sessions/${selected}`, { method: "DELETE" }), "Cancellation requested");
  const current = sessions.find(x => x.ID === selected);
  return <main className={styles.shell} aria-busy={notice === "Working…"}><SessionList sessions={sessions} selected={selected} onSelect={setSelected} onStart={start} /><Timeline task={current?.Task} status={current?.Status} sessionID={selected} turns={turns} onSelect={item => { setDetail(item); history.replaceState(null, "", `?session=${encodeURIComponent(selected)}&event=${item.seq}`); }} onSteer={steer} onCancel={cancel} onBranch={branch} /><DetailPane event={detail} /><div className={styles.notice} aria-live="polite">{notice}</div></main>;
}

function SessionList({ sessions, selected, onSelect, onStart }: { sessions: Session[]; selected: string; onSelect: (id: string) => void; onStart: (task: string) => void }) {
  const [task, setTask] = useState("");
  return <aside><header><span className={styles.mark}>M</span><div><h1>mulch</h1><small>durable traces</small></div></header><form className={styles.start} onSubmit={(e: FormEvent) => { e.preventDefault(); if (task.trim()) { onStart(task.trim()); setTask(""); } }}><label htmlFor="new-task">New session</label><textarea id="new-task" value={task} onChange={e => setTask(e.target.value)} placeholder="Describe the task…" /><button disabled={!task.trim()}>Start session</button></form><nav>{sessions.map(item => <button className={selected === item.ID ? styles.active : ""} onClick={() => onSelect(item.ID)} key={item.ID}><span>{item.Task}</span><small><i className={styles[item.Status]} />{item.Status}</small></button>)}</nav></aside>;
}

function Timeline({ task, status, sessionID, turns, onSelect, onSteer, onCancel, onBranch }: { task?: string; status?: string; sessionID: string; turns: Map<number, TraceEvent[]>; onSelect: (event: TraceEvent) => void; onSteer: (text: string) => void; onCancel: () => void; onBranch: (at: number) => void }) {
  const [steer, setSteer] = useState(""), running = status === "running";
  return <section className={styles.timeline}><div className={styles.heading}><div><small>SESSION</small><h2>{task || "No sessions"}</h2></div><code>{sessionID.slice(0, 12)}</code></div>{sessionID && <div className={styles.controls}><label htmlFor="steer">Steer next turn</label><input id="steer" value={steer} disabled={!running} onChange={e => setSteer(e.target.value)} placeholder={running ? "Add direction…" : "Session is not running"} /><button disabled={!running || !steer.trim()} onClick={() => { onSteer(steer.trim()); setSteer(""); }}>Queue steer</button><button className={styles.danger} disabled={!running} onClick={onCancel}>Cancel</button></div>}{[...turns].map(([turn, items]) => <article key={turn}><div><h3>Turn {turn}</h3><button className={styles.branch} onClick={() => onBranch(items[items.length - 1].seq)}>Branch here</button></div><div className={styles.cards}>{items.map(item => { const steered = item.type === "user.message" && (item.payload as { origin?: string })?.origin === "steer"; return <button key={item.seq} className={`${!item.visible ? styles.dimmed : ""} ${steered ? styles.steered : ""}`} onClick={() => onSelect(item)}><small>#{item.seq}{steered && <b>STEER</b>}</small><strong>{item.type}</strong><time>{new Date(item.created_at).toLocaleTimeString()}</time></button>; })}</div></article>)}</section>;
}
function DetailPane({ event }: { event: TraceEvent | null }) { return <aside className={styles.detail}><div className={styles.heading}><div><small>EVENT DETAIL</small><h2>{event?.type || "Select an event"}</h2></div></div>{event && <pre>{JSON.stringify(event, null, 2)}</pre>}</aside>; }
createRoot(document.getElementById("root")!).render(<Viewer />);
