import test from "node:test";
import assert from "node:assert/strict";
import { traceTree } from "./trace.ts";
import type { TraceEvent } from "../model.ts";
test("trace pairs tools by turn, retains asynchronous evidence and bounds stream chunks",()=>{
  const e=(seq:number,turn:number,type:string,payload:unknown={}):TraceEvent=>({id:seq,session_id:"s",seq,turn,type,payload,visible:true,created_at:""});
  const events=[e(1,1,"assistant.tool_call",{call_id:"same",name:"read"}),e(2,1,"tool.result",{call_id:"same",duration_ms:5}),e(3,2,"assistant.tool_call",{call_id:"same",name:"write"}),e(4,2,"tool.result",{call_id:"same",is_error:true}),e(5,2,"score.health",{turn_scored:1,composite:42}),...Array.from({length:10000},(_,i)=>e(i+6,2,"assistant.delta"))];
  const result=traceTree(events);
  assert.equal(result.length,2);assert.equal(result[0].nodes[0].children[0].event.seq,2);
  assert.equal(result[1].nodes[0].children[0].event.seq,4);
  assert.match(result[1].nodes[1].label,/scored turn 1/);
  assert.equal(result[1].nodes.length,2);assert.equal(result[1].deltas,10000);
});
