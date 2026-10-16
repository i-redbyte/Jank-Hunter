const {test}=require('node:test');
const assert=require('node:assert/strict');
const {threadEndpointObservation}=require('./templates/thread-snapshot.js');
const {attemptsAtThreadObservation}=require('./templates/worker-attempts.js');
const snapshot=(time,tid,start=7,basis='observation_clock')=>({TimeMS:time,TimeBasis:basis,Snapshot:{interval_ms:100,scan_duration_ms:2,threads:[{tid,start_ticks:start}]}});
test('endpoint navigation requires the same session and observed TID',()=>{
 const processes=[{Scope:'other',Snapshots:[snapshot(10,42)]},{Scope:'session',Snapshots:[snapshot(12,99),snapshot(20,42)]}];
 assert.deepEqual(threadEndpointObservation(processes,{scope:'session',tid:'42',time:10}),{process:1,point:1,distance:10});
 assert.equal(threadEndpointObservation(processes,{scope:'absent',tid:'42',time:10}),null);
 assert.equal(threadEndpointObservation(processes,{scope:'session',tid:'',time:10}),null);
});
test('stale and unanchored observations do not establish a temporal candidate',()=>{
 const processes=[{Scope:'session',Snapshots:[snapshot(1000,42),snapshot(10,42,8,'record_time')]}];
 assert.equal(threadEndpointObservation(processes,{scope:'session',tid:'42',time:10}),null);
});
test('equidistant TID generations remain candidates, never an exact generation join',()=>{
 const processes=[{Scope:'session',Snapshots:[snapshot(10,42,7),snapshot(30,42,8)]}];
 assert.deepEqual(threadEndpointObservation(processes,{scope:'session',tid:'42',time:20}),{process:0,point:0,distance:10});
});
test('reverse navigation matches endpoint TID and bounded observation time only',()=>{
 const a={Scope:'s',StartThreadID:'42',StartUnixMS:10,EndThreadID:'99',EndUnixMS:100};
 const items=[a,{...a,Scope:'other'},{...a,StartUnixMS:1000},{...a,StartThreadID:'99'}];
 assert.deepEqual(attemptsAtThreadObservation(items,{scope:'s',tid:'42',time:12,radius:10}),[a]);
 assert.deepEqual(attemptsAtThreadObservation(items,{scope:'s',tid:'42',time:12,radius:null}),[]);
 assert.deepEqual(attemptsAtThreadObservation(items,{scope:'s',tid:'99',time:99,radius:2}),items.filter(a=>a.Scope==='s'));
});

test('unanchored elapsed endpoints cannot be matched to calendar snapshots',()=>{
 const a={Scope:'s',StartThreadID:'42',StartMS:12};
 assert.deepEqual(attemptsAtThreadObservation([a],{scope:'s',tid:'42',time:12,radius:10}),[]);
});
