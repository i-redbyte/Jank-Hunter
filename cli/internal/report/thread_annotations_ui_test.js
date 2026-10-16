const {test}=require('node:test');
const assert=require('node:assert/strict');
const {threadAnnotationView,threadAnnotationSnapshot}=require('./templates/thread-snapshot.js');
test('annotations stay in selected scope and page to fifty rows',()=>{
 const items=Array.from({length:1000},(_,i)=>({Scope:i%2?'one':'two',TimeMS:1000-i,Kind:'worker_start'}));
 const view=threadAnnotationView(items,'one','worker_start',1);
 assert.equal(view.total,500);assert.equal(view.rows.length,50);assert.ok(view.rows.every(v=>v.Scope==='one'));
 assert.ok(view.rows[0].TimeMS<view.rows[49].TimeMS);
 assert.equal(threadAnnotationView(items,'one','ui_stall',9).rows.length,0);
});
test('navigation uses observed snapshot times and reports distance',()=>{
 const points=[{TimeBasis:'record_time',TimeMS:99},{TimeBasis:'observation_clock',TimeMS:80},{TimeBasis:'observation_clock',TimeMS:120}];
 assert.deepEqual(threadAnnotationSnapshot(points,100),{point:1,distance:20});
 assert.equal(threadAnnotationSnapshot([points[0]],100),null);
});
