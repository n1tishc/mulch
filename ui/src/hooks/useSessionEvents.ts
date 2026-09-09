import { useEffect, useState } from "react";
import type { TraceEvent } from "../model.ts";
import { EventBuffer } from "../state/events.ts";
import { api, sessionPath } from "../api/client.ts";

export function useSessionEvents(id: string) {
  const [snapshot, setSnapshot] = useState<{ id:string; events:TraceEvent[] }>({id:"",events:[]});
  const [connection,setConnection] = useState("Connecting");
  useEffect(() => {
    if (!id) { setSnapshot({id:"",events:[]}); setConnection("Ready"); return; }
    let disposed = false, socket: WebSocket | undefined, retry: ReturnType<typeof setTimeout> | undefined;
    let pending: ReturnType<typeof setTimeout> | undefined, attempts = 0;
    const buffer = new EventBuffer(id);
    const publish = () => { if (!pending) pending = setTimeout(() => { pending=undefined; if (!disposed) setSnapshot({id,events:buffer.snapshot()}); }, 50); };
    const connect = async () => {
      if (disposed) return;
      try {
        // Backfill on reconnect fills sequence gaps before following live data.
        const history = await api<TraceEvent[]>(`${sessionPath(id)}/events?from=${buffer.cursor+1}`);
        if (disposed) return;
        for (const event of history || []) buffer.add(event);
        publish();
        socket = new WebSocket(`${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/ws/sessions/${encodeURIComponent(id)}?from=${buffer.cursor+1}&follow=1`);
        socket.onopen = () => { if (!disposed) { attempts=0; setConnection("Connected"); } };
        socket.onmessage = message => { if (disposed) return; try { const event=JSON.parse(message.data) as TraceEvent; const gap=event.seq>buffer.cursor+1; buffer.add(event); publish(); if(gap){setConnection("Recovering missing events");socket?.close();} } catch { setConnection("Invalid stream; reconnecting"); socket?.close(); } };
        socket.onclose = schedule;
      } catch { schedule(); }
    };
    const schedule = () => { if (disposed) return; setConnection("Reconnecting"); retry=setTimeout(() => void connect(), Math.min(5000,500*2**attempts++)); };
    setConnection("Connecting"); void connect();
    return () => { disposed=true; clearTimeout(retry); clearTimeout(pending); socket?.close(); };
  },[id]);
  return {events:snapshot.id===id?snapshot.events:[],connection};
}
