const {test}=require('node:test');
const assert=require('node:assert/strict');
const {threadResourceView,threadResourceRelation}=require('./templates/thread-resources.js');

test('resource evidence is scoped to one process session and recorded completion TID',()=>{
 const rows=[
  {Scope:'a',CompletionTID:'151',Kind:'http',Source:'Feed.load',Target:'GET /feed',StartMS:100,EndMS:120,StartDerived:true},
  {Scope:'a',CompletionTID:'152',Kind:'file_io',Source:'Cache.write',StartMS:130,EndMS:150,StartDerived:true},
  {Scope:'b',CompletionTID:'151',Kind:'database',Source:'Dao.load',StartMS:100,EndMS:110,StartDerived:true},
 ];
 assert.deepEqual(threadResourceView(rows,'a','151','feed',0).rows,[rows[0]]);
 assert.equal(threadResourceView(rows,'a','151','',0).total,1);
 assert.equal(threadResourceView(rows,'a','151','/feed',0).total,1);
 assert.equal(threadResourceView(rows,'b','151','',0).total,1);
});

test('worker attempt shows only overlapping resource candidates in the same session',()=>{
 const rows=[
  {Scope:'a',CompletionTID:'151',StartMS:100,EndMS:120},
  {Scope:'a',CompletionTID:'152',StartMS:130,EndMS:150},
  {Scope:'a',CompletionTID:'153',StartMS:180,EndMS:190},
  {Scope:'b',CompletionTID:'151',StartMS:110,EndMS:115},
 ];
 assert.deepEqual(threadResourceView(rows,'a','','',0,{start:115,end:160}).rows,rows.slice(0,2));
 assert.equal(threadResourceView(rows,'a','','',0,{start:160,end:170}).total,0);
});

test('shared application operation context is distinct from temporal overlap',()=>{
 const window={start:100,end:200,operationID:'77'};
 assert.equal(threadResourceRelation({OperationID:'77'},window),'same_application_operation');
 assert.equal(threadResourceRelation({OperationID:'88'},window),'temporal_overlap');
 assert.equal(threadResourceRelation({OperationID:''},window),'temporal_overlap');
 assert.equal(threadResourceRelation({OperationID:'77'},null),'none');
});

test('unknown resource start is a completion point for temporal candidate filtering',()=>{
 const row={Scope:'a',CompletionTID:'151',StartMS:null,EndMS:120};
 assert.equal(threadResourceView([row],'a','','',0,{start:100,end:130}).total,1);
 assert.equal(threadResourceView([row],'a','','',0,{start:130,end:150}).total,0);
});
