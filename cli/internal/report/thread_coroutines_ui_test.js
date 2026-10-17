const {test}=require('node:test');
const assert=require('node:assert/strict');
const {coroutineSegmentView,coroutineIntervalText,coroutineThreadCandidate}=require('./templates/thread-coroutines.js');

test('running observation does not imply segment completion',()=>{
 const running={State:'running_observed',StartedElapsedMS:100,EndedElapsedMS:null,ObservedThroughElapsedMS:120};
 assert.match(coroutineIntervalText(running),/конец неизвестен/);
 assert.equal(coroutineIntervalText({State:'completed',StartedElapsedMS:100,EndedElapsedMS:130}),'100-130 мс');
});

test('coroutine history filters by exact session and Java thread identity',()=>{
 const rows=[{Scope:'a',Owner:'app.Feed.load',Token:'81',EntryJavaThreadID:'11',ExitJavaThreadID:'22',EntryTID:'151',ExitTID:'252',StartedElapsedMS:100},
  {Scope:'b',Owner:'app.Feed.load',Token:'81',EntryJavaThreadID:'11',ExitJavaThreadID:'11',StartedElapsedMS:100}];
 assert.deepEqual(coroutineSegmentView(rows,'a','22','Feed',0).rows,[rows[0]]);
 assert.equal(coroutineSegmentView(rows,'a','33','',0).total,0);
 assert.equal(coroutineSegmentView(rows,'a','','252',0,'252').total,1);
 assert.equal(coroutineSegmentView(rows,'a','','',0,'999').total,0);
});

test('kernel TID is a scoped observed Java mapping, not an inferred owner',()=>{
 const segment={Scope:'a',EntryJavaThreadID:'11',StartedElapsedMS:100};
 const snapshot=(scope,id,tid,elapsed)=>({Scope:scope,Snapshots:[{TimeBasis:'observation_clock',
  Snapshot:{elapsed_ms:elapsed,threads:[{tid,java:{id}}]}}]});
 assert.deepEqual(coroutineThreadCandidate([snapshot('b','11',99,100),snapshot('a','11',42,110)],segment),
  {process:1,point:0,tid:'42',distance:10});
 assert.equal(coroutineThreadCandidate([snapshot('a','12',42,100)],segment),null);
});

test('coroutine snapshot candidate must agree with recorded entry TID',()=>{
 const segment={Scope:'a',EntryJavaThreadID:'11',EntryTID:'42',StartedElapsedMS:100};
 const processes=[{Scope:'a',Snapshots:[
  {TimeBasis:'observation_clock',Snapshot:{elapsed_ms:101,threads:[{tid:99,java:{id:'11'}}]}},
  {TimeBasis:'observation_clock',Snapshot:{elapsed_ms:108,threads:[{tid:42,java:{id:'11'}}]}}
 ]}];
 assert.deepEqual(coroutineThreadCandidate(processes,segment),{process:0,point:1,tid:'42',distance:8});
 assert.equal(coroutineThreadCandidate([{Scope:'a',Snapshots:[processes[0].Snapshots[0]]}],segment),null);
});

test('thread filter requires Java ID and Linux TID on the same segment endpoint',()=>{
 const crossed={Scope:'a',Owner:'crossed',EntryJavaThreadID:'11',EntryTID:'100',
  ExitJavaThreadID:'22',ExitTID:'200'};
 const actual={Scope:'a',Owner:'actual',EntryJavaThreadID:'11',EntryTID:'200',
  ExitJavaThreadID:'22',ExitTID:'300'};
 assert.deepEqual(coroutineSegmentView([crossed,actual],'a','11','',0,'200').rows,[actual]);
});
