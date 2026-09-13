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
  const [inspector,setInspector]=useState(new URLSearchParams(location.search).get("view")==="trace"),[tab,setTab]=useState<InspectorTab>("trace"),[eventSeq,setEventSeq]=useState(Number(new URLSearchParams(location.search).get("event"))||0);
  const [paths,setPaths]=useState<string[]>([]),[adding,setAdding]=useState(false),[directory,setDirectory]=useState(""),[deleting,setDeleting]=useState(false);
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
  const refreshPaths=useCallback(async()=>setPaths(await api<string[]>("/api/workspaces")),[]);
  useEffect(()=>{void refreshPaths().catch(()=>{});},[refreshPaths]);
  useEffect(()=>{let live=true;void api<WebConfig>("/api/config").then(c=>{if(live){setConfig(c);setWorkspace(c.workspace);}},e=>{if(live)setError(String(e));});return()=>{live=false;};},[]);
  useEffect(()=>{let live=true;const poll=()=>void refresh().catch(()=>{if(live)setNotice("Session list unavailable; retrying…");});poll();const timer=setInterval(poll,3000);return()=>{live=false;clearInterval(timer);};},[refresh]);
  useEffect(()=>{setDetail(undefined);if(!id)return;let live=true;const poll=async()=>{try{const d=await api<SessionDetail>(sessionPath(id));if(live){setDetail(d);setAccepted("");}}catch{if(live)setNotice("Loading saved session…");}};void poll();const timer=setInterval(()=>void poll(),1000);return()=>{live=false;clearInterval(timer);};},[id]);
  useEffect(()=>{history.replaceState(null,"",id?`?session=${encodeURIComponent(id)}${eventSeq?`&event=${eventSeq}`:""}`:location.pathname);},[id,eventSeq]);
  useEffect(()=>{const onKey=(e:KeyboardEvent)=>{if((e.metaKey||e.ctrlKey)&&e.key==="k"){e.preventDefault();setPalette(v=>!v);}};window.addEventListener("keydown",onKey);return()=>window.removeEventListener("keydown",onKey);},[]);
  useEffect(()=>{if(eventSeq)setInspector(true);},[]);
  const select=(next:string)=>{setID(next);setEventSeq(0);setDrawer(false);setError("");setNotice("");};
  const newChat=(path?:string)=>{const next=path||current?.session.Workdir||workspace,newKey=`new:${next}`;setWorkspace(next);setDrafts(d=>({...d,[newKey]:""}));try{localStorage.removeItem(`mulch:draft:${newKey}`);}catch{/* Storage may be disabled. */}persistPending();select("");};
  const setDraft=(value:string)=>{setDrafts(d=>({...d,[key]:value}));try{localStorage.setItem(`mulch:draft:${key}`,value);}catch{/* In-memory drafts remain available. */}};
  const persistPending=(value?:Pending)=>{pending.current=value;try{if(value)sessionStorage.setItem("mulch:pending",JSON.stringify(value));else sessionStorage.removeItem("mulch:pending");}catch{/* Identity remains in memory for network retries. */}};
  const updateRuntime=async(next:{provider?:string;model?:string;mode?:string})=>{const updated=await api<WebConfig>("/api/config",{method:"PATCH",body:body({provider:next.provider||"",model:next.model||"",mode:next.mode||""})});setConfig(updated);return updated;};
  const slash=async(text:string)=>{
    const [name,...rest]=text.trim().split(/\s+/),arg=rest.join(" ");
    if(name==="/new"){setDraft("");newChat();setNotice("New conversation. Previous history remains saved.");return true;}
    if(name==="/model"&&!arg){setNotice(`Models for ${config?.provider||"current provider"}: ${config?.providers?.find(p=>p.name===config.provider)?.models.join(", ")||config?.model||"none configured"}`);setDraft("");return true;}
    if(name==="/provider"&&!arg){setNotice(`Configured providers: ${config?.providers?.map(p=>p.name).join(", ")||config?.provider||"none"}`);setDraft("");return true;}
    if(name==="/api-key"){setError("API keys are entered in the local terminal so they never pass through the browser. Run /api-key in mulch, or use mulch config set api-key.");setDraft("");return true;}
    if((name==="/model"||name==="/provider"||name==="/mode")&&arg){const field=name.slice(1) as "model"|"provider"|"mode";const updated=await updateRuntime({[field]:arg});setNotice(`Now using ${updated.provider||"provider"} · ${updated.model} · ${updated.mode}. Conversation history is unchanged.`);setDraft("");return true;}
    return false;
  };
  const send=async()=>{
    if(sending.current||!draft.trim()||running)return;
    if(draft.trim().startsWith("/")){setBusy(true);setError("");try{if(await slash(draft))return;setError(`Unknown command “${draft.trim().split(/\s+/)[0]}”. Try /new, /model, /provider, /mode, or /api-key.`);}catch(e){setError(String(e));}finally{setBusy(false);}return;}
    if(!config?.ready)return;
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
  const commands:Command[]=[{label:"New conversation · /new",run:()=>newChat()},{label:"Switch or resume session",run:()=>setDrawer(true)},{label:"Rename session",disabled:!current?.can_rename,run:()=>{setLabel(current?.session.Label||"");setRename(true);}},{label:"Show execution trace",run:()=>openTab("trace")},{label:"Delete conversation",disabled:!current?.can_delete,run:()=>setDeleting(true)},{label:"Add workspace",disabled:!config?.can_manage,run:()=>setAdding(true)},{label:"Show status and health",run:()=>openTab("health")},{label:"Inspect context",run:()=>openTab("context")},{label:"Show repairs",run:()=>openTab("repairs")},{label:"Show raw events",run:()=>openTab("raw")},{label:"Stop task",disabled:!current?.can_stop,run:stop},...(config?.providers||[]).flatMap(p=>[{label:`Provider: ${p.name}${p.name===config?.provider?" · current":""}`,run:()=>void updateRuntime({provider:p.name}).then(setConfig)},...p.models.map(model=>({label:`Model: ${model}${model===config?.model&&p.name===config?.provider?" · current":""}`,run:()=>void updateRuntime({provider:p.name,model}).then(setConfig)}))]),...flattenSessions(sessions).slice(0,30).map(s=>({label:`Open: ${s.Label||s.Task||s.ID}`,run:()=>select(s.ID)}))];
  const reason=!config?.ready?(config?.read_only_reason||"Read-only: configure a provider to send messages."):current?.owner==="external"?"Running in another process. Control it from its terminal.":id&&!current?"Loading session capabilities…":connection==="Reconnecting"?"Reconnecting to the event stream…":"";
  const sidebar=<SessionSidebar sessions={sessions} selected={id} workspace={workspace} paths={paths} canManage={!!config?.can_manage} onSelect={select} onNew={newChat} onAdd={()=>{setDirectory("");setAdding(true);setDrawer(false);}}/>;
  const evidence=<RepairInspector events={events} selected={events.find(e=>e.seq===eventSeq)} onSelect={inspect} tab={tab} setTab={setTab} onSession={select}/>;
  const effectiveMode=id?(payload(recordedConfig(events)).mode||config?.mode||"unknown"):(config?.mode||"—");
  return <main className={`app-shell ${inspector?"with-inspector":""}`}><aside className="sidebar">{sidebar}</aside><section className="workspace"><header className="workspace-header"><div className="workspace-title"><button className="quiet mobile-sessions" onClick={()=>setDrawer(true)} aria-label="Open sessions"><Icon name="menu"/></button><div><small>WORKSPACE</small><strong title={current?.session.Workdir||workspace}>{(current?.session.Workdir||workspace).split("/").filter(Boolean).at(-1)||"Choose a workspace"}</strong></div></div><div className="header-actions"><span className="connection" role="status"><i className={connection==="Connected"||!id?"connected":""}/>{id?connection:"Local"}</span><button className="quiet commands-button" onClick={()=>setPalette(true)}>Commands <kbd>⌘ K</kbd></button><button className={`secondary ${inspector?"selected":""}`} onClick={()=>setInspector(v=>!v)} aria-expanded={inspector}>Inspect</button></div></header><div className="session-heading"><div><h1>{current?.session.Label||current?.session.Task||"New conversation"}</h1><p>{config?.provider&&<span>{config.provider} · </span>}{config?.model||current?.session.Model||"Loading model…"} <span>· {effectiveMode} mode</span> {id&&<span>· {current?.session.Status||"loading"}</span>}</p></div>{current?.can_delete&&<button className="quiet" onClick={()=>setDeleting(true)}>Delete chat</button>}{current?.can_rename&&<button className="quiet" onClick={()=>{setLabel(current.session.Label||"");setRename(true);}}>Rename</button>}</div>
    {!config?.ready&&config?.read_only_reason&&<div className="setup-notice">{config.read_only_reason}</div>}
    {!config?.ready&&config&&!config.read_only_reason&&<div className="setup-notice"><strong>Connect a model to start coding</strong><p>Run <code>/api-key</code> in the Mulch terminal, or use <code>mulch config</code> to save providers and models. Saved conversations remain available here.</p></div>}
    {id?<Conversation key={id} events={events} onInspect={inspect} running={running}/>:<div className="welcome"><div className="welcome-mark" aria-hidden="true">m</div><h2>What are we working on?</h2><p>Build, investigate, and fix with a conversation you can inspect.</p><label className="field workspace-field">Working directory<input value={workspace} onChange={e=>setWorkspace(e.target.value)} placeholder="/path/to/project" spellCheck={false}/></label><div className="suggestions">{["Explain how this repository is structured.","Find a bug and propose the smallest safe fix.","Review the current changes and missing tests."].map(text=><button className="suggestion" key={text} onClick={()=>setDraft(text)}>{text}<Icon name="diagonal"/></button>)}</div></div>}
    {error&&<div className="error-banner" role="alert">{error}<button className="quiet" onClick={()=>setError("")} aria-label="Dismiss error"><Icon name="close"/></button></div>}<div className="status-notice" role="status">{notice}</div><Composer draft={draft} setDraft={setDraft} onSend={()=>void send()} onStop={stop} onSteer={steer} disabled={!draft.trim().startsWith("/")&&(!config?.ready||!workspace.trim()||!!id&&!current?.can_resume)} busy={busy} running={running} canStop={!!current?.can_stop} canSteer={!!current?.can_steer} reason={reason}/></section>
    {adding&&<Dialog label="Add workspace" onClose={()=>setAdding(false)}><form onSubmit={e=>{e.preventDefault();void act(async()=>{const result=await api<{path:string}>("/api/workspaces",{method:"POST",body:body({path:directory})});await refreshPaths();setAdding(false);newChat(result.path);},"Workspace added");}}><p>Choose an existing project directory. Chats in each folder use that directory for their tools.</p><label className="field">Directory path<input autoFocus required value={directory} onChange={e=>setDirectory(e.target.value)} placeholder="/absolute/path/to/project"/></label><button className="primary" disabled={busy}>Add directory</button>{error&&<p role="alert">{error}</p>}</form></Dialog>}
    {deleting&&<Dialog label="Delete conversation?" onClose={()=>setDeleting(false)}><p>Permanently delete “{current?.session.Label||current?.session.Task||id}” and its candidate branches from saved history? Project files stay on disk.</p><div className="dialog-actions"><button className="secondary" onClick={()=>setDeleting(false)}>Keep conversation</button><button className="stop" disabled={busy} onClick={()=>void act(async()=>{await api(`${sessionPath(id)}/history`,{method:"DELETE"});const root=flattenSessions(sessions).find(s=>s.ID===id);const removed=root?flattenSessions([root]).map(s=>s.ID):[id];for(const sid of removed){try{localStorage.removeItem(`mulch:draft:${sid}`);}catch{/* Storage may be disabled. */}}setDrafts(d=>Object.fromEntries(Object.entries(d).filter(([key])=>!removed.includes(key))));if(pending.current&&removed.some(sid=>pending.current!.path.startsWith(sessionPath(sid))))persistPending();setDeleting(false);select("");},"Conversation deleted. Project files remain unchanged.")}>Delete permanently</button></div>{error&&<p role="alert">{error}</p>}</Dialog>}
    {inspector&&!narrow&&<aside className="inspector desktop-inspector"><header><div><h2>Context inspector</h2></div><button className="quiet" onClick={()=>setInspector(false)} aria-label="Close inspector"><Icon name="close"/></button></header>{evidence}</aside>}
    {inspector&&narrow&&<Dialog label="Context inspector" className="inspector-dialog" onClose={()=>setInspector(false)}>{evidence}</Dialog>}{drawer&&<Dialog label="Conversations" className="session-dialog" onClose={()=>setDrawer(false)}>{sidebar}</Dialog>}{palette&&<CommandPalette commands={commands} onClose={()=>setPalette(false)}/>} {rename&&<Dialog label="Rename session" onClose={()=>setRename(false)}><form onSubmit={e=>{e.preventDefault();void act(async()=>{await api(sessionPath(id),{method:"PATCH",body:body({label})});setDetail(d=>d?{...d,session:{...d.session,Label:label}}:d);setRename(false);},"Session renamed");}}><label className="field">Session name<input autoFocus maxLength={200} value={label} onChange={e=>setLabel(e.target.value)}/></label><button className="primary" disabled={busy}>Save name</button></form></Dialog>}</main>;
}

function useNarrow() {
  const [narrow,setNarrow]=useState(()=>matchMedia("(max-width: 1199px)").matches);
  useEffect(()=>{const media=matchMedia("(max-width: 1199px)"),change=()=>setNarrow(media.matches);media.addEventListener("change",change);return()=>media.removeEventListener("change",change);},[]);
  return narrow;
}
