import { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import styles from "./viewer.module.css";

type Session = { ID: string; ParentID: string; Task: string; Status: string; CreatedAt: string };
type TraceEvent = { id: number; session_id: string; seq: number; turn: number; type: string; payload: unknown; visible: boolean; created_at: string; [key: string]: unknown };

function Viewer() {
  const [sessions, setSessions] = useState<Session[]>([]);
  const query = new URLSearchParams(location.search);
  const [selected, setSelected] = useState(query.get("session") || "");
  const [events, setEvents] = useState<TraceEvent[]>([]);
  const [detail, setDetail] = useState<TraceEvent | null>(null);
  useEffect(() => { fetch("/api/sessions").then(r => r.json()).then((items: Session[]) => { setSessions(items); setSelected(x => x || items[0]?.ID || ""); }); }, []);
  useEffect(() => {
    if (!selected) return;
    const controller = new AbortController();
    setEvents([]);
    setDetail(null);
    history.replaceState(null, "", `?session=${encodeURIComponent(selected)}`);
    fetch(`/api/sessions/${selected}/events?from=1`, { signal: controller.signal }).then(r => r.json()).then((items: TraceEvent[]) => {
      setEvents(items);
      const seq = query.get("session") === selected ? Number(query.get("event")) : Number.NaN;
      setDetail(items.find(item => item.seq === seq) || null);
    }).catch(error => { if (error.name !== "AbortError") throw error; });
    return () => controller.abort();
  }, [selected]);
  const turns = useMemo(() => events.reduce((grouped, event) => {
    const items = grouped.get(event.turn) || [];
    items.push(event);
    grouped.set(event.turn, items);
    return grouped;
  }, new Map<number, TraceEvent[]>()), [events]);
  const selectEvent = (event: TraceEvent) => {
    setDetail(event);
    history.replaceState(null, "", `?session=${encodeURIComponent(selected)}&event=${event.seq}`);
  };
  return <main className={styles.shell}><SessionList sessions={sessions} selected={selected} onSelect={setSelected} /><Timeline task={sessions.find(x => x.ID === selected)?.Task} sessionID={selected} turns={turns} onSelect={selectEvent} /><DetailPane event={detail} /></main>;
}

function SessionList({ sessions, selected, onSelect }: { sessions: Session[]; selected: string; onSelect: (id: string) => void }) {
  return <aside><header><span className={styles.mark}>M</span><div><h1>mulch</h1><small>durable traces</small></div></header><nav>{sessions.map(session => <button className={selected === session.ID ? styles.active : ""} onClick={() => onSelect(session.ID)} key={session.ID}><span>{session.Task}</span><small><i className={styles[session.Status]} />{session.Status}</small></button>)}</nav></aside>;
}

function Timeline({ task, sessionID, turns, onSelect }: { task?: string; sessionID: string; turns: Map<number, TraceEvent[]>; onSelect: (event: TraceEvent) => void }) {
  return <section className={styles.timeline}><div className={styles.heading}><div><small>SESSION</small><h2>{task || "No sessions"}</h2></div><code>{sessionID.slice(0, 12)}</code></div>{[...turns].map(([turn, items]) => <article key={turn}><h3>Turn {turn}</h3><div className={styles.cards}>{items.map(event => <button key={event.seq} className={!event.visible ? styles.dimmed : ""} onClick={() => onSelect(event)}><small>#{event.seq}</small><strong>{event.type}</strong><time>{new Date(event.created_at).toLocaleTimeString()}</time></button>)}</div></article>)}</section>;
}

function DetailPane({ event }: { event: TraceEvent | null }) {
  return <aside className={styles.detail}><div className={styles.heading}><div><small>EVENT DETAIL</small><h2>{event?.type || "Select an event"}</h2></div></div>{event && <pre>{JSON.stringify(event, null, 2)}</pre>}</aside>;
}
createRoot(document.getElementById("root")!).render(<Viewer />);
