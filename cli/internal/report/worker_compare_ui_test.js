const {test}=require('node:test');
const assert=require('node:assert/strict');
const {workerSchedulePage}=require('./templates/worker-compare.js');
test('schedule pages have bounded rows and clamp out-of-range pages',()=>{
 const rows=Array.from({length:256},(_,i)=>i);
 const first=workerSchedulePage(rows,-1),last=workerSchedulePage(rows,999);
 assert.equal(first.rows.length,50);assert.equal(first.rows[0],0);
 assert.equal(last.rows.length,6);assert.equal(last.rows[5],255);assert.equal(last.pages,6);
});
test('empty process selection cannot show the first unrelated process',()=>{
 const {workerComparisonSelection}=require('./templates/worker-compare.js');
 const rows=[{Process:'app'}];
 assert.equal(workerComparisonSelection(rows,''),null);
 assert.equal(workerComparisonSelection(rows,'0'),rows[0]);
});
