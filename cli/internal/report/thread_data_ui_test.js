const {test}=require('node:test');
const assert=require('node:assert/strict');
const {threadAnalysisData}=require('./templates/thread-snapshot.js');

test('thread views reuse one parsed analysis per report section',()=>{
  const payload={textContent:'{"Resources":[{"Scope":"session"}]}'};
  const section={closest:()=>section,querySelector:()=>payload};
  const child={closest:()=>section};
  const first=threadAnalysisData(section);
  payload.textContent='invalid after first parse';
  assert.strictEqual(threadAnalysisData(child),first);
  assert.equal(first.Resources[0].Scope,'session');
});
