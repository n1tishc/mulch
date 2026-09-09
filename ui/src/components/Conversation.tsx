import { memo, useEffect, useMemo, useRef, useState } from "react";
import Markdown from "react-markdown";
import { Icon } from "./Icon.tsx";
import remarkGfm from "remark-gfm";
import { conversation, payload, type ConversationRow } from "../state/events.ts";
import type { TraceEvent } from "../model.ts";

function Copy({text}:{text:string}) {
  const [state,setState]=useState("Copy");
  return <button className="quiet copy" onClick={() => void navigator.clipboard.writeText(text).then(() => setState("Copied"), () => setState("Copy unavailable"))}>{state}</button>;
}
function CodeBlock({children}:{children:React.ReactNode}) {
  const ref=useRef<HTMLPreElement>(null),[copied,setCopied]=useState(false);
  return <div className="code-block"><pre ref={ref}>{children}</pre><button className="quiet copy" onClick={()=>void navigator.clipboard.writeText(ref.current?.textContent||"").then(()=>setCopied(true))}>{copied?"Copied":"Copy code"}</button></div>;
}
const Message = memo(function Message({row,onInspect}:{row:ConversationRow;onInspect:(e:TraceEvent)=>void}) {
  const p=payload(row.event), result=payload(row.result);
  if(row.kind==="tool") return <details className="tool-call"><summary><span><Icon name="chevron"/>{row.text}</span><small>{!row.result ? "Running" : result.is_error || result.cancelled || result.timed_out ? "Failed" : "Completed"}{row.result && ` · ${result.duration_ms ?? 0}ms`}</small></summary><div className="tool-content"><h4>Input</h4><pre>{JSON.stringify(p.input,null,2)}</pre><h4>Output</h4><pre>{row.result ? String(result.output || "No output").slice(0,12000) : "Waiting for tool result…"}</pre>{String(result.output || "").length>12000 && <p>Preview truncated. The raw event retains the complete result.</p>}<button className="quiet" onClick={()=>onInspect(row.result || row.event)}>Inspect event #{(row.result || row.event).seq}</button></div></details>;
  if(row.kind==="repair") return <button className="repair-notice" onClick={()=>onInspect(row.event)}><strong>Context repair</strong><span>{row.text}</span><Icon name="right"/></button>;
  if(row.kind==="end") return <p className="task-end">Task {row.text} <span>· {Math.round(Number(p.wall_ms || 0)/1000)}s · not independently graded</span></p>;
  return <article className={`message ${row.kind}`}><div className="message-label">{row.kind==="user" ? "You" : "Mulch"}<button className="quiet event-link" onClick={()=>onInspect(row.event)}>#{row.event.seq}</button></div><div className="prose"><Markdown skipHtml remarkPlugins={[remarkGfm]} components={{img:({alt})=><span>[Image: {alt || "omitted"}]</span>,pre:({children})=><CodeBlock>{children}</CodeBlock>,code:({children,className})=><code className={className}>{children}</code>}}>{row.text.slice(0,50000)}</Markdown></div>{row.kind==="assistant" && <Copy text={row.text}/>}</article>;
});

export function Conversation({events,onInspect,running}:{events:TraceEvent[];onInspect:(e:TraceEvent)=>void;running:boolean}) {
  const rows=useMemo(()=>conversation(events),[events]);
  const [limit,setLimit]=useState(100), [following,setFollowing]=useState(true);
  const scroller=useRef<HTMLDivElement>(null);
  useEffect(()=>{if(following && scroller.current) scroller.current.scrollTop=scroller.current.scrollHeight;},[events,following]);
  return <div className="conversation" ref={scroller} onScroll={e=>{const el=e.currentTarget;setFollowing(el.scrollHeight-el.scrollTop-el.clientHeight<100);}} aria-label="Conversation"><div className="conversation-inner">{rows.length>limit && <button className="quiet" onClick={()=>setLimit(n=>n+100)}>Show earlier messages ({rows.length-limit})</button>}{rows.slice(-limit).map(row=><Message key={row.key} row={row} onInspect={onInspect}/>)}{running && <div className="working" role="status"><span className="working-dot"/> Working in your workspace…</div>}{!following && <button className="latest" onClick={()=>setFollowing(true)}>Jump to latest</button>}</div></div>;
}
