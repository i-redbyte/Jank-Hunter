const test=require('node:test');
const assert=require('node:assert/strict');
const {workChainPage,workChainRootPage,workChainForJob,workChainRootsForJob}=require('./templates/work-chain.js');

test('declared dependencies require the same exact session',()=>{
  const edges=[{ParentUUID:'a',ChildUUID:'b',Scope:'one',Process:'app',Edge:{CallerSource:'x'}},
    {ParentUUID:'c',ChildUUID:'b',Scope:'two',Process:'app',Edge:{CallerSource:'x'}}];
  assert.deepEqual(workChainForJob(edges,'one','b').incoming,['a']);
  assert.equal(workChainPage(edges,'','one',0).total,1);
});

test('job card bounds broad dependency lists',()=>{
  const edges=Array.from({length:50},(_,index)=>({ParentUUID:String(index),ChildUUID:'b',Scope:'one'}));
  const result=workChainForJob(edges,'one','b');
  assert.equal(result.incoming.length,20);
  assert.equal(result.incomingOmitted,30);
});

test('unique root stays a graph declaration with exact scope and bounded page',()=>{
  const roots=Array.from({length:130},(_,index)=>({UUID:String(index),Scope:'one',Process:'app',Root:{UniqueName:'sync',Policy:1,CallerSource:'app#root'}}));
  roots.push({UUID:'other',Scope:'two',Process:'app',Root:{UniqueName:'sync',Policy:1,CallerSource:'app#root'}});
  const page=workChainRootPage(roots,'sync','one',2);
  assert.equal(page.total,130);
  assert.equal(page.rows.length,30);
  assert.equal(page.rows[0].UUID,'100');
  assert.equal(workChainRootsForJob(roots,'one','0').observations.length,1);
  assert.equal(workChainRootsForJob(roots,'two','0').observations.length,0);
});
