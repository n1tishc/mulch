import { useMemo, useState } from "react";
import type { TraceEvent } from "../model.ts";
import { payload } from "../state/events.ts";
import { traceTree, type TraceNode } from "../state/trace.ts";
export function TraceTree({events,onSelect,onSession}:{events:TraceEvent[];onSelect:(e:TraceEvent)=>void;onSession:(id:string)=>void}) {
  const turns=useMemo(()=>traceTree(events),[events]),[limit,setLimit]=useState(20);
  const bySeq=useMemo(()=>new Map(events.map(e=>[e.seq,e])),[events]);
  const node=(n:TraceNode):React.ReactNode=>{
    const p=payload(n.event),branches=Array.isArray(p.branches)?p.branches:[];
    return <li key={n.event.seq}><button className="trace-event" onClick={()=>onSelect(n.event)}><span className="trace-seq">#{n.event.seq}</span><span>{n.label}</span></button>
      {n.event.type==="intervene.fire"&&<div className="trace-links">{[[p.score_seq,"Supporting score"],[p.context_seq,"Context change"]].map(([seq,label])=>{const e=bySeq.get(Number(seq));return e?<button className="quiet" key={String(label)} onClick={()=>onSelect(e)}>{label} #{seq}</button>:<span className="muted" key={String(label)}>{label}: unrecorded</span>;})}</div>}
      {branches.length>0&&<ul>{branches.map((id:string,i:number)=><li key={id}><button className="trace-event" onClick={()=>onSession(id)}><span>Candidate · {p.actions?.[i]||id.slice(0,8)}{p.winner===id?" · selected":""}</span></button></li>)}</ul>}
      {n.children.length>0&&<ul>{n.children.map(node)}</ul>}</li>;
  };
  return <section className="trace-tree" aria-label="Execution trace"><h3>Execution trace</h3><p className="muted">Recorded turns, model calls, tools, and repairs. Expand a turn, then select an event for evidence.</p>
    {!events.length&&<p>Send a task to see its execution here. Repairs appear only when triggered.</p>}
    {turns.length>limit&&<button className="quiet" onClick={()=>setLimit(n=>n+20)}>Show 20 earlier turns</button>}
    {turns.slice(-limit).map((t,i,shown)=><details className="trace-turn" key={t.turn} open={i===shown.length-1||undefined}><summary>{t.turn===0?"Session setup":`Turn ${t.turn}`}<span>{t.nodes.length} steps{t.deltas?` · ${t.deltas} stream chunks`:""}</span></summary><TraceSteps nodes={t.nodes} render={node}/></details>)}
    <p className="muted">Turns group recorded activity, not causal order across asynchronous scorers. Repair links use recorded sequence IDs. Private model reasoning is not available.</p></section>;
}
function TraceSteps({nodes,render}:{nodes:TraceNode[];render:(n:TraceNode)=>React.ReactNode}) {
  const [limit,setLimit]=useState(100);
  return <><ul>{nodes.slice(0,limit).map(render)}</ul>{nodes.length>limit&&<button className="quiet" onClick={()=>setLimit(n=>n+100)}>Show 100 more steps</button>}</>;
}
