const {test}=require('node:test');const assert=require('node:assert/strict');
const {workSubmissionPage}=require('./templates/work-submissions.js');
test('submission history filters unique names and stays bounded',()=>{
 const rows=Array.from({length:1000},(_,i)=>({UUID:String(i),Scope:i%2?'one':'two',Process:'app',Submission:{UniqueName:'sync',Policy:1}}));
 const view=workSubmissionPage(rows,'sync',1,'one',1);
 assert.equal(view.total,500);assert.equal(view.rows.length,50);
 assert.equal(workSubmissionPage(rows,'missing',0,'',0).rows.length,0);
});
test('caller source remains searchable and legacy absence stays unknown',()=>{
 const {workSubmissionSourceText}=require('./templates/work-submissions.js');
 const item={UUID:'u',Scope:'s',Process:'p',Submission:{Policy:0,CallerSource:'example.MyCalls.submit',SourceTruncated:true}};
 assert.equal(workSubmissionPage([item],'mycalls',0,'',0).total,1);
 assert.match(workSubmissionSourceText(item.Submission),/усеч/);
 assert.match(workSubmissionSourceText({}),/неизвест/);
 assert.match(workSubmissionSourceText({CallerSource:'unknown'}),/неизвест/);
});
test('explicit builder hints distinguish captured request from unavailable metadata',()=>{
 const {workSubmissionHintsText}=require('./templates/work-submissions.js');
 assert.match(workSubmissionHintsText({BackoffPolicy:1,RequestedBackoffMS:15000,QuotaPolicy:2}),/линейный, запрос 15000 мс.*отбросить/);
 assert.match(workSubmissionHintsText({}),/не наблюдался/);
});
