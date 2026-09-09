import { useState } from "react";
import { Icon } from "./Icon.tsx";
import type { WebSession } from "../api/client.ts";
type Props={sessions:WebSession[];selected:string;workspace:string;paths:string[];canManage:boolean;onSelect:(id:string)=>void;onNew:(path?:string)=>void;onAdd:()=>void};
export function SessionSidebar({sessions,selected,workspace,paths,canManage,onSelect,onNew,onAdd}:Props) {
  const [query,setQuery]=useState("");
  const row=(s:WebSession,depth=0):React.ReactNode=><div key={s.ID}>{(!query||`${s.Label} ${s.Task}`.toLowerCase().includes(query.toLowerCase()))&&<button className={`session-row ${selected===s.ID?"active":""}`} style={{paddingLeft:14+depth*12}} aria-current={selected===s.ID?"page":undefined} onClick={()=>onSelect(s.ID)}><span>{depth>0&&<Icon name="return"/>}{s.Label||s.Task||"Untitled conversation"}</span><small>{s.Status} · {new Date(s.CreatedAt).toLocaleDateString(undefined,{month:"short",day:"numeric"})}</small></button>}{s.Children?.map(child=>row(child,depth+1))}</div>;
  const groups=[...new Set([...paths,workspace,...sessions.map(s=>s.Workdir)].filter(Boolean))];
  return <><div className="sidebar-brand"><span className="mark">m</span><strong>mulch</strong><small>local workspace</small></div><button className="new-session" onClick={()=>onNew()}><span>New conversation</span><Icon name="add"/></button><label className="session-search"><span className="sr-only">Search sessions</span><input placeholder="Search conversations" value={query} onChange={e=>setQuery(e.target.value)}/></label><div className="workspace-nav-heading"><strong>Workspaces</strong>{canManage&&<button className="quiet" onClick={onAdd} aria-label="Add workspace"><Icon name="add"/></button>}</div><nav aria-label="Sessions">{groups.map(path=>{
    const items=sessions.filter(s=>s.Workdir===path);
    return <details className="workspace-group" key={path} open><summary title={path}><span>{path.split(/[\\/]/).filter(Boolean).at(-1)||path}</span><small>{items.length}</small></summary><p className="workspace-path" title={path}>{path}</p><button className="quiet workspace-new" onClick={()=>onNew(path)}>New chat here <Icon name="add"/></button>{items.map(s=>row(s))}{!items.length&&<p className="sidebar-empty">No conversations in this folder.</p>}</details>;
  })}{!groups.length&&<p className="sidebar-empty">Add a project directory to get started.</p>}</nav><footer className="sidebar-footer">Local files. Durable history.<br/><span>Open the trace to see each recorded step.</span></footer></>;
}
