const test=require('node:test');
const assert=require('node:assert/strict');
const {threadSnapshotDiff}=require('./templates/thread-snapshot.js');
const row=(java)=>({tid:12,start_ticks:'44',name:'os-name',os_state:'S',java});
test('Java rename and state stay distinct from OS observations',()=>{
 const diff=threadSnapshotDiff([row({id:'7',name:'before',state:'WAITING'})],[row({id:'7',name:'after',state:'RUNNABLE'})]);
 assert.equal(diff.renamed.length,0);assert.equal(diff.changed.length,0);
 assert.deepEqual(diff.javaRenamed,['before → after']);
 assert.deepEqual(diff.javaChanged,['after: WAITING → RUNNABLE']);
});
test('missing or different Java identity is not a state transition',()=>{
 for(const java of [null,{id:'8',name:'new-object',state:'RUNNABLE'}]){
 const diff=threadSnapshotDiff([row({id:'7',name:'before',state:'WAITING'})],[row(java)]);
 assert.equal(diff.javaRenamed.length,0);assert.equal(diff.javaChanged.length,0);
 }
});
test('TID reuse is appearance and absence instead of rename',()=>{
 const diff=threadSnapshotDiff([row(null)],[{...row(null),start_ticks:'45',name:'new'}]);
 assert.equal(diff.added.length,1);assert.equal(diff.gone.length,1);assert.equal(diff.renamed.length,0);
});
