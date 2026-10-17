const {test}=require('node:test');
const assert=require('node:assert/strict');
const {workerOrchestrationSignals}=require('./templates/worker-signals.js');
const attempt=(extra={})=>({Scope:'s',UUID:'u',Worker:'app.Sync',StartMS:10,EndMS:20,Coverage:'start_and_finish_observed',Outcome:'success',...extra});
test('retry evidence is scoped to unambiguous job identity, not attempt counter',()=>{
 const data={Attempts:[attempt({Outcome:'retry'}),attempt({Outcome:'retry',StartMS:30,EndMS:40}),attempt({Scope:'other',Outcome:'retry'}),attempt({IdentityAmbiguous:true,Outcome:'retry'})]};
 const rows=workerOrchestrationSignals(data).signals.filter(s=>s.kind==='repeated_retry');
 assert.equal(rows.length,1);assert.equal(rows[0].count,2);assert.deepEqual(rows[0].attemptIndices,[0,1]);assert.equal(rows[0].scope,'s');
});
test('overlap requires complete unambiguous intervals in same scope and class',()=>{
 const rows=[attempt(),attempt({UUID:'v',StartMS:15,EndMS:25}),attempt({Scope:'other'}),attempt({StartMS:15,EndMS:null,Coverage:'missing_finish'}),attempt({Ambiguous:true})];
 const signal=workerOrchestrationSignals({Attempts:rows}).signals.find(s=>s.kind==='overlap');
 assert.equal(signal.count,2);assert.equal(signal.observedMS,15);assert.deepEqual(signal.attemptIndices,[0,1]);
});
test('touching intervals, unknown scope/class and invalid timestamps cannot prove overlap',()=>{
 for(const rows of [[attempt(),attempt({StartMS:20,EndMS:30})],[attempt({Scope:''}),attempt({Scope:''})],[attempt({Worker:'unknown'}),attempt({Worker:'unknown'})],[attempt({StartMS:NaN}),attempt({EndMS:5})]])assert.equal(workerOrchestrationSignals({Attempts:rows}).signals.length,0);
});
test('WorkInfo and missing lifecycle never become a running interval or retry evidence',()=>{
 const a=attempt({StartMS:null,EndMS:null,Coverage:'missing_start',Outcome:'retry',LastJobObservation:{State:1,WorkInfoRunAttempt:8}});
 assert.equal(workerOrchestrationSignals({Attempts:[a,a]}).signals.length,0);
 assert.deepEqual(workerOrchestrationSignals(null).signals,[]);
});
test('input stays unchanged and losses and bounded signal output remain explicit',()=>{
 const rows=Array.from({length:140},(_,i)=>attempt({Scope:'s'+Math.floor(i/2),Outcome:'retry'}));
 const data={Attempts:rows,DroppedAttempts:3,DroppedLifecycleEvents:4,DroppedIdentityBindings:5};const before=JSON.stringify(data);
 const result=workerOrchestrationSignals(data);
 assert.equal(JSON.stringify(data),before);assert.equal(result.signals.length,64);assert.equal(result.omittedSignals,76);assert.equal(result.incomplete,true);
 assert.deepEqual(result.losses,{attempts:3,lifecycle:4,identity:5});
});
test('large peak keeps exact count with bounded evidence and no invented duration',()=>{
 const rows=Array.from({length:2000},(_,i)=>attempt({UUID:'u'+i,StartMS:i,EndMS:5000}));
 const signal=workerOrchestrationSignals({Attempts:rows}).signals[0];
 assert.equal(signal.kind,'overlap');assert.equal(signal.count,2000);assert.equal(signal.observedMS,1999);assert.equal(signal.attemptIndices.length,16);
});
test('many retry jobs cannot hide observed class overlaps',()=>{
 const rows=Array.from({length:140},(_,i)=>attempt({Scope:'s'+Math.floor(i/2),Outcome:'retry',StartMS:10,EndMS:20}));
 const result=workerOrchestrationSignals({Attempts:rows});
 assert.equal(result.signals.length,64);assert.equal(result.omittedSignals,76);
 assert.ok(result.signals.some(signal=>signal.kind==='repeated_retry'));
 assert.ok(result.signals.some(signal=>signal.kind==='overlap'));
});
test('retry without UUID or with ambiguous lifecycle cannot be linked as one job',()=>{
 for(const extra of [{UUID:''},{Ambiguous:true},{IdentityAmbiguous:true}]) {
 const rows=[attempt({...extra,Outcome:'retry',StartMS:1,EndMS:2}),attempt({...extra,Outcome:'retry',StartMS:3,EndMS:4})];
 assert.equal(workerOrchestrationSignals({Attempts:rows}).signals.length,0);
 }
});
