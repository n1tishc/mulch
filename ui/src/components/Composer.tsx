import { useRef } from "react";
import { Icon } from "./Icon.tsx";

type ComposerProps={draft:string;setDraft:(v:string)=>void;onSend:()=>void;onStop:()=>void;onSteer:()=>void;disabled:boolean;busy:boolean;running:boolean;canStop:boolean;canSteer:boolean;reason:string};

export function Composer({draft,setDraft,onSend,onStop,onSteer,disabled,busy,running,canStop,canSteer,reason}:ComposerProps) {
  const input=useRef<HTMLTextAreaElement>(null);
  return <div className="composer-wrap">
    <form className="composer" onSubmit={e=>{e.preventDefault();onSend();}}>
      <textarea ref={input} aria-label="Message" placeholder={running?"Add guidance for the next turn…":"Message Mulch, or type / for commands…"} value={draft} onChange={e=>setDraft(e.target.value)} onKeyDown={e=>{if(e.key==="Enter"&&!e.shiftKey&&!e.nativeEvent.isComposing){e.preventDefault();if(!disabled&&!busy&&draft.trim()&&!running)onSend();}}} maxLength={100000} rows={3}/>
      <div className="composer-actions"><small>{reason || "Enter to send · Shift+Enter for a new line"}</small><div>{running ? <>{canSteer&&<button type="button" className="secondary" disabled={busy||!draft.trim()} onClick={onSteer}>Steer next turn</button>}<button type="button" className="stop" disabled={!canStop||busy} onClick={onStop}>{busy?"Stopping…":"Stop task"}</button></>:<button type="submit" className="primary" disabled={disabled||busy||!draft.trim()}>{busy?"Sending…":"Send message"}<Icon name="up"/></button>}</div></div>
    </form>
    <p className="composer-note">Works on local files. Type <code>/model</code> or <code>/provider</code> to switch without losing this conversation.</p>
  </div>;
}
