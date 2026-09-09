import { useCallback, useEffect, useRef, useState } from "react";
import { api, body, flattenSessions, sessionPath, type SessionDetail, type WebConfig, type WebSession } from "./api/client.ts";
import { useSessionEvents } from "./hooks/useSessionEvents.ts";
import { Conversation } from "./components/Conversation.tsx";
import { Composer } from "./components/Composer.tsx";
import { SessionSidebar } from "./components/SessionSidebar.tsx";
import { CommandPalette, type Command } from "./components/CommandPalette.tsx";
import { Dialog } from "./components/Dialog.tsx";
import { RepairInspector, type InspectorTab } from "./components/RepairInspector.tsx";
import { payload, recordedConfig } from "./state/events.ts";
import type { TraceEvent } from "./model.ts";
import { Icon } from "./components/Icon.tsx";

function readDraft(key:string) {try{return localStorage.getItem(`mulch:draft:${key}`)||"";}catch{return "";}}
type Pending={path:string;content:Record<string,unknown>;draft:string;workspace:string};
function readPending():Pending|undefined {try{return JSON.parse(sessionStorage.getItem("mulch:pending")||"null")||undefined;}catch{return undefined;}}

export function App() {
  const [config,setConfig]=useState<WebConfig>(),[sessions,setSessions]=useState<WebSession[]>([]),[id,setID]=useState(new URLSearchParams(location.search).get("session")||"");
  const [detail,setDetail]=useState<SessionDetail>(),[workspace,setWorkspace]=useState(""),[drafts,setDrafts]=useState<Record<string,string>>({});
  const [error,setError]=useState(""),[notice,setNotice]=useState(""),[busy,setBusy]=useState(false),[accepted,setAccepted]=useState("");
  const [inspector,setInspector]=useState(false),[tab,setTab]=useState<InspectorTab>("health"),[eventSeq,setEventSeq]=useState(Number(new URLSearchParams(location.search).get("event"))||0);
  const [palette,setPalette]=useState(false),[drawer,setDrawer]=useState(false),[rename,setRename]=useState(false),[label,setLabel]=useState("");
  const pending=useRef<Pending|undefined>(readPending()),sending=useRef(false);
  const observed=useRef({id:"",lastEnd:0,initialized:false}),submitted=useRef({id:"",after:0});
  const {events,connection}=useSessionEvents(id);
  useEffect(()=>{
    const seen=observed.current;
    if(seen.id!==id){seen.id=id;seen.lastEnd=0;seen.initialized=false;}
    if(!id||!events.length)return;
    const ends=events.filter(e=>e.type==="session.end"),last=ends.at(-1)?.seq||0;
    // Initial replay is silent, except for a task just submitted in this tab.
    const after=seen.initialized?seen.lastEnd:submitted.current.id===id?submitted.current.after:last;
    const fresh=ends.filter(e=>e.seq>after);
    seen.initialized=true;seen.lastEnd=last;
    if(fresh.length)setNotice(fresh.map(e=>`Task ${String(payload(e).status||"finished").replaceAll("_"," ")}.`).join(" "));
  },[id,events]);
  const narrow=useNarrow();
  const key=id||`new:${workspace}`,draft=drafts[key]??readDraft(key),current=detail?.session.ID===id?detail:undefined;
  const running=current?.session.Status==="running"||current?.owner==="daemon"||accepted===id&&!!id;
  const refresh=useCallback(async()=>{const list=await api<WebSession[]>("/api/sessions");setSessions(list||[]);},[]);
  useEffect(()=>{let live=true;void api<WebConfig>("/api/config").then(c=>{if(live){setConfig(c);setWorkspace(c.workspace);}},e=>{if(live)setError(String(e));});return()=>{live=false;};},[]);
  useEffect(()=>{let live=true;const poll=()=>void refresh().catch(()=>{if(live)setNotice("Session list unavailable; retrying…");});poll();const timer=setInterval(poll,3000);return()=>{live=false;clearInterval(timer);};},[refresh]);
  useEffect(()=>{setDetail(undefined);if(!id)return;let live=true;const poll=async()=>{try{const d=await api<SessionDetail>(sessionPath(id));if(live){setDetail(d);setAccepted("");}}catch{if(live)setNotice("Loading saved session…");}};void poll();const timer=setInterval(()=>void poll(),1000);return()=>{live=false;clearInterval(timer);};},[id]);
  useEffect(()=>{history.replaceState(null,"",id?`?session=${encodeURIComponent(id)}${eventSeq?`&event=${eventSeq}`:""}`:location.pathname);},[id,eventSeq]);
  useEffect(()=>{const onKey=(e:KeyboardEvent)=>{if((e.metaKey||e.ctrlKey)&&e.key==="k"){e.preventDefault();setPalette(v=>!v);}};window.addEventListener("keydown",onKey);return()=>window.removeEventListener("keydown",onKey);},[]);
  useEffect(()=>{if(eventSeq)setInspector(true);},[]);
  const select=(next:string)=>{setID(next);setEventSeq(0);setDrawer(false);setError("");setNotice("");};
  const setDraft=(value:string)=>{setDrafts(d=>({...d,[key]:value}));try{localStorage.setItem(`mulch:draft:${key}`,value);}catch{/* In-memory drafts remain available. */}};
  const persistPending=(value?:Pending)=>{pending.current=value;try{if(value)sessionStorage.setItem("mulch:pending",JSON.stringify(value));else sessionStorage.removeItem("mulch:pending");}catch{/* Identity remains in memory for network retries. */}};
  const send=async()=>{
    if(sending.current||!draft.trim()||!config?.ready||running)return;
    const path=id?`${sessionPath(id)}/resume`:"/api/sessions";
    if(!pending.current||pending.current.path!==path||pending.current.draft!==draft||pending.current.workspace!==workspace)persistPending({path,content:{...(id?{text:draft.trim()}:{task:draft.trim(),opts:{workdir:workspace}}),request_id:crypto.randomUUID()},draft,workspace});
    sending.current=true;setBusy(true);setError("");
    try{const result=await api<{id:string}>(path,{method:"POST",body:body(pending.current!.content)});submitted.current={id:result.id,after:id?(events.at(-1)?.seq||0):0};persistPending();setDraft("");setAccepted(result.id);select(result.id);setNotice("Task accepted");void refresh().catch(()=>{});}catch(e){setError(`${String(e)} Your draft is saved. Retry with the same message to reuse this submission ID.`);}finally{sending.current=false;setBusy(false);}
  };
  const act=async(operation:()=>Promise<unknown>,message:string)=>{if(sending.current)return;sending.current=true;setBusy(true);setError("");try{await operation();setNotice(message);void refresh().catch(()=>{});}catch(e){setError(String(e));}finally{sending.current=false;setBusy(false);}};
  const stop=()=>void act(()=>api(sessionPath(id),{method:"DELETE"}),"Stop requested. Completed edits remain in the workspace.");
  const steer=()=>void act(async()=>{await api(`${sessionPath(id)}/steer`,{method:"POST",body:body({text:draft.trim()})});setDraft("");},"Guidance submitted for the next turn");
  const inspect=useCallback((e:TraceEvent)=>{setEventSeq(e.seq);setTab(e.type.startsWith("intervene.")||e.type.startsWith("race.")?"repairs":e.type.startsWith("context.")||e.type==="llm.request"?"context":e.type==="score.health"?"health":"raw");setInspector(true);},[]);
  const openTab=(t:InspectorTab)=>{setTab(t);setInspector(true);};
  const commands:Command[]=[{label:"New conversation",run:()=>select("")},{label:"Switch or resume session",run:()=>setDrawer(true)},{label:"Rename session",disabled:!current?.can_rename,run:()=>{setLabel(current?.session.Label||"");setRename(true);}},{label:"Show status and health",run:()=>openTab("health")},{label:"Inspect context",run:()=>openTab("context")},{label:"Show repairs",run:()=>openTab("repairs")},{label:"Show raw events",run:()=>openTab("raw")},{label:"Stop task",disabled:!current?.can_stop,run:stop},...flattenSessions(sessions).slice(0,30).map(s=>({label:`Open: ${s.Label||s.Task||s.ID}`,run:()=>select(s.ID)}))];
  const reason=!config?.ready?(config?.read_only_reason||"Read-only: configure a provider to send messages."):current?.owner==="external"?"Running in another process. Control it from its terminal.":id&&!current?"Loading session capabilities…":connection==="Reconnecting"?"Reconnecting to the event stream…":"";
  const sidebar=<SessionSidebar sessions={sessions} selected={id} onSelect={select} onNew={()=>select("")}/>;
  const evidence=<RepairInspector events={events} selected={events.find(e=>e.seq===eventSeq)} onSelect={inspect} tab={tab} setTab={setTab} onSession={select}/>;
  const effectiveMode=id?(payload(recordedConfig(events)).mode||"unknown"):(config?.mode||"—");
  return <main className={`app-shell ${inspector?"with-inspector":""}`}><aside className="sidebar">{sidebar}</aside><section className="workspace"><header className="workspace-header"><div className="workspace-title"><button className="quiet mobile-sessions" onClick={()=>setDrawer(true)} aria-label="Open sessions"><Icon name="menu"/></button><div><small>WORKSPACE</small><strong title={current?.session.Workdir||workspace}>{(current?.session.Workdir||workspace).split("/").filter(Boolean).at(-1)||"Choose a workspace"}</strong></div></div><div className="header-actions"><span className="connection" role="status"><i className={connection==="Connected"||!id?"connected":""}/>{id?connection:"Local"}</span><button className="quiet commands-button" onClick={()=>setPalette(true)}>Commands <kbd>⌘ K</kbd></button><button className={`secondary ${inspector?"selected":""}`} onClick={()=>setInspector(v=>!v)} aria-expanded={inspector}>Inspect</button></div></header><div className="session-heading"><div><h1>{current?.session.Label||current?.session.Task||"New conversation"}</h1><p>{current?.session.Model||config?.model||"Loading model…"} <span>· {effectiveMode} mode</span> {id&&<span>· {current?.session.Status||"loading"}</span>}</p></div>{current?.can_rename&&<button className="quiet" onClick={()=>{setLabel(current.session.Label||"");setRename(true);}}>Rename</button>}</div>
    {!config?.ready&&config?.read_only_reason&&<div className="setup-notice">{config.read_only_reason}</div>}
    {!config?.ready&&config&&!config.read_only_reason&&<div className="setup-notice"><strong>Connect a model to start coding</strong><p>Set <code>MULCH_PROVIDER_API_KEY</code>, <code>MULCH_PROVIDER_BASE_URL</code>, and <code>MULCH_MODEL</code> in the terminal, then restart <code>mulch web</code>. Saved conversations remain available here.</p></div>}
    {id?<Conversation key={id} events={events} onInspect={inspect} running={running}/>:<div className="welcome"><div className="welcome-mark" aria-hidden="true">m</div><h2>What are we working on?</h2><p>Build, investigate, and fix with a conversation you can inspect.</p><label className="field workspace-field">Working directory<input value={workspace} onChange={e=>setWorkspace(e.target.value)} placeholder="/path/to/project" spellCheck={false}/></label><div className="suggestions">{["Explain how this repository is structured.","Find a bug and propose the smallest safe fix.","Review the current changes and missing tests."].map(text=><button className="suggestion" key={text} onClick={()=>setDraft(text)}>{text}<Icon name="diagonal"/></button>)}</div></div>}
    {error&&<div className="error-banner" role="alert">{error}<button className="quiet" onClick={()=>setError("")} aria-label="Dismiss error"><Icon name="close"/></button></div>}<div className="status-notice" role="status">{notice}</div><Composer draft={draft} setDraft={setDraft} onSend={()=>void send()} onStop={stop} onSteer={steer} disabled={!config?.ready||!workspace.trim()||!!id&&!current?.can_resume} busy={busy} running={running} canStop={!!current?.can_stop} canSteer={!!current?.can_steer} reason={reason}/></section>
    {inspector&&!narrow&&<aside className="inspector desktop-inspector"><header><div><h2>Context inspector</h2></div><button className="quiet" onClick={()=>setInspector(false)} aria-label="Close inspector"><Icon name="close"/></button></header>{evidence}</aside>}
    {inspector&&narrow&&<Dialog label="Context inspector" className="inspector-dialog" onClose={()=>setInspector(false)}>{evidence}</Dialog>}{drawer&&<Dialog label="Conversations" className="session-dialog" onClose={()=>setDrawer(false)}>{sidebar}</Dialog>}{palette&&<CommandPalette commands={commands} onClose={()=>setPalette(false)}/>} {rename&&<Dialog label="Rename session" onClose={()=>setRename(false)}><form onSubmit={e=>{e.preventDefault();void act(async()=>{await api(sessionPath(id),{method:"PATCH",body:body({label})});setDetail(d=>d?{...d,session:{...d.session,Label:label}}:d);setRename(false);},"Session renamed");}}><label className="field">Session name<input autoFocus maxLength={200} value={label} onChange={e=>setLabel(e.target.value)}/></label><button className="primary" disabled={busy}>Save name</button></form></Dialog>}</main>;
}

function useNarrow() {
  const [narrow,setNarrow]=useState(()=>matchMedia("(max-width: 1199px)").matches);
  useEffect(()=>{const media=matchMedia("(max-width: 1199px)"),change=()=>setNarrow(media.matches);media.addEventListener("change",change);return()=>media.removeEventListener("change",change);},[]);
  return narrow;
}
