import { useEffect, useRef, type ReactNode } from "react";
import { Icon } from "./Icon.tsx";
export function Dialog({label,onClose,children,className=""}:{label:string;onClose:()=>void;children:ReactNode;className?:string}) {
  const ref=useRef<HTMLDialogElement>(null);
  useEffect(()=>{const previous=document.activeElement as HTMLElement|null;ref.current?.showModal();return()=>{previous?.focus();};},[]);
  return <dialog ref={ref} className={className} aria-label={label} onCancel={onClose} onClick={e=>{if(e.target===e.currentTarget)onClose();}}><div className="dialog-content"><header><h2>{label}</h2><button className="quiet" onClick={onClose} aria-label={`Close ${label}`}>Close <Icon name="close"/></button></header>{children}</div></dialog>;
}
