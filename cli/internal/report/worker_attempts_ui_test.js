const {test} = require('node:test');
const assert = require('node:assert/strict');
const {attemptPlotInterval, filterAttempts} = require('./templates/worker-attempts.js');
test('execution segments preserve 64 bit durations and never total nested CPU',()=>{
 const {attemptExecutionSegmentsText}=require('./templates/worker-attempts.js');
 assert.match(attemptExecutionSegmentsText({}),/не наблюдались/);
 const text=attemptExecutionSegmentsText({ExecutionSegments:[
  {Source:'app.Task#run',TargetTID:'42',DurationNS:'9007199254740993',CPUNS:'0'},
  {Source:'app.Worker#invokeSuspend',DurationNS:'1000000',CPUNS:null},
 ],ExecutionSegmentsOmitted:2,ExecutionEventsOmitted:3});
 assert.match(text,/9007199254\.740/);
 assert.match(text,/CPU 0\.000/);
 assert.match(text,/CPU неизвестно/);
 assert.match(text,/TID 42/);
 assert.match(text,/нельзя складывать/);
 assert.match(text,/пропущено сегментов: 2/);
 assert.match(text,/пропущено событий: 3/);
});
test('unknown starts and missing finishes are points, never invented intervals', () => {
  assert.deepEqual(attemptPlotInterval({StartMS:10,EndMS:null}),{start:10,end:10,kind:'start'});
  assert.deepEqual(attemptPlotInterval({StartMS:null,EndMS:20,DurationMS:10}),{start:20,end:20,kind:'finish'});
  assert.deepEqual(attemptPlotInterval({StartMS:10,EndMS:20}),{start:10,end:20,kind:'interval'});
  assert.deepEqual(attemptPlotInterval({FailureObservedMS:25}),{start:25,end:25,kind:'failure'});
});
test('exception class is searchable without inventing a terminal result',()=>{
 const attempts=[{Worker:'',FailureClass:'app.SyncException',FailureObservedMS:25,Outcome:'unknown',Process:'app'}];
 assert.equal(filterAttempts(attempts,'SyncException','app','unknown',20,30).length,1);
 assert.equal(filterAttempts(attempts,'SyncException','app','failure',20,30).length,0);
});
test('attempt filter combines class, process, outcome and observed interval', () => {
  const items=[{Worker:'app.Sync',Process:'app',Outcome:'retry',StartMS:10,EndMS:20},{Worker:'app.Sync',Process:'remote',Outcome:'unknown',StartMS:30,EndMS:null}];
  assert.equal(filterAttempts(items,'sync','app','retry',15,25).length,1);
  assert.equal(filterAttempts(items,'sync','','',21,29).length,0);
});
test('explicit UUID is searchable independently of private runtime ID',()=>{
 const values=[{Worker:'app.Sync',UUID:'00000000-0000-0000-0000-000000000007',InstanceID:'123',StartMS:1}];
 assert.equal(filterAttempts(values,'00000000-0000-0000-0000-000000000007','','',null,null).length,1);
});
test('job navigation filters attempts by session scope and UUID together',()=>{
 const items=[
  {Scope:'a',UUID:'same',Worker:'app.Sync',StartMS:10},
  {Scope:'b',UUID:'same',Worker:'app.Sync',StartMS:10},
  {Scope:'a',UUID:'same-other',Worker:'app.Sync',StartMS:10},
 ];
 assert.deepEqual(filterAttempts(items,'same','','',null,null,'a','same'),[items[0]]);
 assert.equal(filterAttempts(items,'same','','',null,null,'c','same').length,0);
 assert.equal(filterAttempts(items,'same','','',null,null).length,3);
});
test('job RUNNING never proves the captured attempt is still running',()=>{
 const {attemptJobObservationText}=require('./templates/worker-attempts.js');
 assert.match(attemptJobObservationText(null),/неизвестно/);
 const text=attemptJobObservationText({State:1,ObservedElapsedMS:20,AgeAtCaptureMS:30,Freshness:'stale',AbsentFromLaterCompleteSnapshot:true});
 assert.match(text,/выполняется по последнему наблюдению/);
 assert.match(text,/устаревшее/);assert.match(text,/конкретной попытки неизвестно/);assert.match(text,/позднем полном снимке отсутствует/);
});
test('attempt card labels WorkInfo progress as job observation, never attempt ownership',()=>{
 const {attemptJobProgressText}=require('./templates/worker-attempts.js');
 assert.match(attemptJobProgressText(null),/неизвестны/);
 const text=attemptJobProgressText({ObservedElapsedMS:20,Progress:[{Key:'done',Type:'int',PayloadBytes:4}],OmittedProgressKeys:2});
 assert.match(text,/done/);assert.match(text,/int/);assert.match(text,/4/);
 assert.match(text,/другой попытке/);assert.match(text,/пропущено ключей: 2/);
});
test('direct progress requests are bounded attempt evidence, not persisted WorkInfo',()=>{
 const {attemptDirectProgressText}=require('./templates/worker-attempts.js');
 assert.match(attemptDirectProgressText({}),/не наблюдались/);
 assert.match(attemptDirectProgressText({ProgressOmittedUpdates:3}),/пропущено вызовов: 3/);
 const text=attemptDirectProgressText({ProgressRequests:[{ObservedMS:20,Fields:[{Key:'done',Type:'int',PayloadBytes:4}],OmittedKeys:2}],ProgressOmittedUpdates:3});
 assert.match(text,/done/);assert.match(text,/int/);assert.match(text,/4 байт/);
 assert.match(text,/пропущено ключей: 2/);assert.match(text,/пропущено вызовов: 3/);
 assert.match(text,/не подтверждает сохранение/);
});
test('foreground interval states confirmed entry and unknown actual exit',()=>{
 const {attemptForegroundText}=require('./templates/worker-attempts.js');
 assert.match(attemptForegroundText({}),/не наблюдался/);
 assert.match(attemptForegroundText({ForegroundEnteredMS:20}),/конец наблюдения неизвестен/);
 assert.match(attemptForegroundText({ForegroundEnteredMS:20,ForegroundBoundaryMS:35}),/Фактический выход.*неизвестны/);
});
test('attempt plot uses calendar anchors when both observed endpoints have them',()=>{
 assert.deepEqual(attemptPlotInterval({StartMS:10,EndMS:20,StartUnixMS:100010,EndUnixMS:100020}),{start:100010,end:100020,kind:'interval'});
 assert.deepEqual(attemptPlotInterval({StartMS:10,StartUnixMS:100010}),{start:100010,end:100010,kind:'start'});
 assert.deepEqual(attemptPlotInterval({StartMS:10,EndMS:20,StartUnixMS:100010}),{start:10,end:20,kind:'interval'});
});
