import test from "node:test";
import assert from "node:assert/strict";
import { performance } from "node:perf_hooks";
import { EventBuffer, conversation, contextChanges, recordedConfig, requestContext } from "./events.ts";
import { dimensions } from "./health.ts";
import type { TraceEvent } from "../model.ts";
const e=(seq:number,type:string,payload:unknown={},turn=1):TraceEvent=>({id:seq,seq,session_id:"s",type,payload,turn,visible:true,created_at:"2026-09-08T00:00:00Z"});
test("backfill fills gaps, deduplicates and rejects cross-session events",()=>{
  const b=new EventBuffer("s");b.add(e(3,"session.end"));assert.equal(b.cursor,0);b.add(e(1,"user.message"));assert.equal(b.cursor,1);b.add(e(2,"assistant.message"));b.add(e(2,"assistant.message"));assert.equal(b.cursor,3);assert.equal(b.snapshot().length,3);b.add(e(4,"user.message",{},2));assert.equal(b.cursor,4);assert.throws(()=>b.add({...e(5,"x"),session_id:"other"}));
});
test("streamed deltas are replaced by final text; repeated call IDs match by turn",()=>{
  const rows=conversation([e(1,"assistant.delta",{text:"hel"}),e(2,"assistant.delta",{text:"lo"}),e(3,"assistant.message",{text:"hello"}),e(4,"assistant.tool_call",{call_id:"a",name:"read"}),e(5,"tool.result",{call_id:"a",output:"first"}),e(6,"assistant.tool_call",{call_id:"a",name:"read"},2),e(7,"tool.result",{call_id:"a",output:"second"},2)]);
  assert.equal(rows[0].text,"hello");assert.equal(rows[1].result?.seq,5);assert.equal(rows[2].result?.seq,7);
});
test("context evidence uses occurrences and historical visibility, not payload equality",()=>{
  const first={...e(1,"tool.result",{output:"identical"}),visible:false},second=e(2,"tool.result",{output:"identical"});
  const request=e(3,"llm.request",{visible_event_seqs:[1,2]}),prune=e(4,"context.visibility",{changes:[{seq:2,from:true,to:false}]});
  assert.deepEqual(requestContext([first,second],request).map(x=>x.seq),[1,2]);
  assert.equal(contextChanges([first,second],prune)[0].event?.seq,2);
  assert.equal(contextChanges([first,second],e(5,"context.compact",{replaced_seqs:[1,2],summary:"short"})).length,3);
  assert.equal(contextChanges([],e(6,"context.inject",{text:"anchor"}))[0].before,"absent");
});
test("dimensions normalize once and preserve unknown/reused/unavailable evidence",()=>{
  const d=dimensions(e(1,"score.health",{composite:80,saturation:.8,coherence:1,freshness:{coherence:"reused"}}));
  assert.equal(d[0].value,80);assert.equal(d[0].freshness,"unknown");assert.equal(d[3].value,100);assert.equal(d[3].freshness,"reused");assert.equal(d[2].value,undefined);assert.equal(d[2].freshness,"unavailable");
  const configs=[e(1,"run.config",{mode:"plain"}),e(3,"run.config",{mode:"race"})];assert.equal(recordedConfig(configs,2)?.seq,1);assert.equal(recordedConfig(configs)?.seq,3);
});
test("10,000-event replay stays bounded and reduces in under 100ms locally",()=>{
  const b=new EventBuffer("s"),start=performance.now();for(let i=1;i<=10000;i++)b.add(e(i,"assistant.delta",{text:"x"}));const rows=conversation(b.snapshot());const ms=performance.now()-start;
  assert.equal(b.snapshot().length,10000);assert.equal(rows.length,1);assert.equal(rows[0].text.length,10000);assert.ok(ms<100,`replay took ${ms.toFixed(1)}ms`);
});
