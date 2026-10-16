const test=require('node:test');
const assert=require('node:assert/strict');
const {threadOriginText,threadMainText}=require('./templates/thread-snapshot.js');
test('kernel start displays uncertainty and never substitutes first observation',()=>{
 assert.match(threadOriginText({KernelStart:null}),/неизвест/);
 const value={FromElapsedMS:90,ByElapsedMS:100,FromUnixMS:4090,ByUnixMS:4100,BeforeCollection:true};
 assert.match(threadOriginText({KernelStart:value}),/90.*100/);
 assert.match(threadOriginText({KernelStart:value}),/4090.*4100/);
 assert.match(threadOriginText({KernelStart:value}),/до зафиксированной границы сбора/);
 assert.match(threadOriginText({KernelStart:{...value,BeforeCollection:false}}),/после зафиксированной границы сбора/);
 assert.match(threadOriginText({KernelStart:{...value,BeforeCollection:null}}),/неизвест/);
 assert.match(threadOriginText({KernelStart:{...value,FromUnixMS:null,ByUnixMS:null}}),/календарное время неизвестно/);
});
test('main-thread identity has an explicit unknown state',()=>{
 assert.match(threadMainText({MainThread:true}),/TID = PID/);
 assert.match(threadMainText({MainThread:false}),/не главный/);
 assert.match(threadMainText({MainThread:null}),/неизвест/);
 assert.match(threadMainText(),/неизвест/);
});
