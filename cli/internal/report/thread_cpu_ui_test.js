const {test}=require('node:test');
const assert=require('node:assert/strict');
const {threadCPUText}=require('./templates/thread-snapshot.js');
test('unknown and zero measured CPU remain distinct',()=>{
 assert.equal(threadCPUText(null),'неизвестно');
 assert.match(threadCPUText({CorePercent:0,ElapsedMS:1000,TickMS:10}),/0\.0%/);
 assert.match(threadCPUText({CorePercent:30,ElapsedMS:1000,TickMS:10}),/1000 мс/);
});
test('Java identity and state remain separate from kernel state and creation time',()=>{
 const {threadJavaText}=require('./templates/thread-snapshot.js');
 assert.match(threadJavaText(null),/неизвестно/);
 const text=threadJavaText({id:'9007199254740993',name:'java',state:'RUNNABLE',daemon:true,first_observed_elapsed_nanos:'123456789012345678',source:'Task.run'});
 assert.match(text,/9007199254740993/);
 assert.match(text,/RUNNABLE/);
 assert.match(text,/создание и start неизвестны/);
});
test('thread search includes verified Java name ID and run source',()=>{
 const {threadSearchText}=require('./templates/thread-snapshot.js');
 const text=threadSearchText({name:'kernel',tid:42,java:{id:'9007199254740993',name:'Java name',source:'app.Owner.run'}});
 assert.match(text,/java name/); assert.match(text,/9007199254740993/); assert.match(text,/app.owner.run/);
 assert.match(threadSearchText({name:'native',tid:3}),/native/);
});
test('old snapshots do not imply zero Java identity losses',()=>{
 const {threadJavaLossText}=require('./templates/thread-snapshot.js');
 assert.match(threadJavaLossText({version:1}),/неизвестно/);
 assert.equal(threadJavaLossText({version:2,dropped_java_identities:0}),'0');
 assert.equal(threadJavaLossText({version:2,dropped_java_identities:3}),'3');
});
