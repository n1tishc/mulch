import { useState } from "react";
import { Icon } from "./Icon.tsx";
import { Dialog } from "./Dialog.tsx";
export type Command={label:string;run:()=>void;disabled?:boolean};
export function CommandPalette({commands,onClose}:{commands:Command[];onClose:()=>void}) {
  const [query,setQuery]=useState("");
  const matches=commands.filter(c=>c.label.toLowerCase().includes(query.toLowerCase()));
  return <Dialog label="Commands" className="palette" onClose={onClose}><input autoFocus aria-label="Find a command" placeholder="Find a command…" value={query} onChange={e=>setQuery(e.target.value)} onKeyDown={e=>{if(e.key==="Enter"){e.preventDefault();const c=matches.find(c=>!c.disabled);if(c){onClose();c.run();}}}}/><div className="command-list">{matches.map(c=><button key={c.label} disabled={c.disabled} onClick={()=>{onClose();c.run();}}>{c.label}<Icon name="return"/></button>)}{!matches.length&&<p>No matching commands.</p>}</div><small>Tab to choose · Enter to run · Escape to close</small></Dialog>;
}
