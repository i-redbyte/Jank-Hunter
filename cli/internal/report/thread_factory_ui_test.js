const {test}=require('node:test');
const assert=require('node:assert/strict');
const {threadFactoryIndex,threadFactoryText}=require('./templates/thread-snapshot.js');
const returned=(scope,id,factory,nanos)=>({Scope:scope,Observation:{stage:3,java_id:id,factory,observed_elapsed_nanos:nanos}});
test('factory return requires exact session, 64-bit Java ID and prior observation time',()=>{
 const id='9007199254740993';
 const events=[returned('a',id,'app.Factory','1000000'),returned('b',id,'other.Factory','1'),returned('a','9007199254740992','wrong.Factory','1'),returned('a',id,'future.Factory','3000000')];
 const index=threadFactoryIndex(events,'a');
 assert.match(threadFactoryText(index,{java:{id}},2),/app\.Factory/);
 assert.doesNotMatch(threadFactoryText(index,{java:{id}},2),/other|wrong|future/);
 assert.match(threadFactoryText(index,{java:{id}},0),/неизвестн/);
 assert.match(threadFactoryText(index,{java:{id}},4),/несколько/);
 assert.match(threadFactoryText(index,{name:'app.Factory'},4),/неизвестн/);
});
test('factory index bounds distinct Java IDs and exposes omitted coverage',()=>{
 const index=threadFactoryIndex([returned('a','1','f1','1'),returned('a','2','f2','1')],'a',1);
 assert.equal(index.omitted,1);
 assert.match(threadFactoryText(index,{java:{id:'2'}},2),/неизвестн/);
});
test('later overflow cannot rewrite an earlier factory observation',()=>{
 const events=Array.from({length:5},(_,i)=>returned('a','7','Factory'+i,String((i+1)*1000000)));
 const index=threadFactoryIndex(events,'a');
 assert.match(threadFactoryText(index,{java:{id:'7'}},2),/Factory0/);
 assert.doesNotMatch(threadFactoryText(index,{java:{id:'7'}},2),/несколько/);
 assert.match(threadFactoryText(index,{java:{id:'7'}},6),/несколько/);
});
test('early omitted factory remains unknown when retained events are later',()=>{
 const events=[10,11,12,13,1].map((ms,i)=>returned('a','7','Factory'+i,String(ms*1000000)));
 const index=threadFactoryIndex(events,'a');
 assert.match(threadFactoryText(index,{java:{id:'7'}},2),/индекс фабрик неполон/);
});
