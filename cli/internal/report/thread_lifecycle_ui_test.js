const {test}=require('node:test');
const assert=require('node:assert/strict');
const {threadLifecyclePage,threadLifecycleStageText}=require('./templates/thread-lifecycle.js');
test('run start-return and factory-return never claim kernel creation',()=>{
 assert.match(threadLifecycleStageText(1),/наблюдение run/);
 assert.match(threadLifecycleStageText(2),/start вернулся/);
 assert.match(threadLifecycleStageText(3),/вернула Thread/);
 assert.match(threadLifecycleStageText(0),/неизвестно/);
});
test('point-event table bounds rows and searches exact preserved Java ID',()=>{
 const values=Array.from({length:123},(_,i)=>({Process:'app',Observation:{stage:1,java_id:String(i),name:'short',source:'Task.run'}}));
 assert.equal(threadLifecyclePage(values,'',0,100).rows.length,23);
 assert.equal(threadLifecyclePage(values,'',0,-1).rows.length,50);
 assert.equal(threadLifecyclePage(values,'122',1,0).total,1);
 assert.equal(threadLifecyclePage(values,'',2,0).total,0);
 assert.equal(threadLifecyclePage([],'',0,0).pages,1);
});
