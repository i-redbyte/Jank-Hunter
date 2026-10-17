const {test}=require('node:test');const assert=require('node:assert/strict');
const s=require('./templates/work-schedules.js');
test('schedule explains flex and its provenance without inventing defaults',()=>{
 assert.match(s.scheduleRequestedText({RequestedPeriodMS:1800000,RequestedFlexMS:300000}),/30 мин/);
 assert.match(s.scheduleRequestedText({RequestedPeriodMS:1800000,RequestedFlexMS:300000}),/последних 5 мин/);
 assert.match(s.scheduleRequestedText({RequestedPeriodMS:1800000}),/без отдельного окна/);
 assert.match(s.scheduleRequestedText({Periodic:true}),/не наблюдал/);
 assert.match(s.scheduleRequestedText({RequestedInitialDelayMS:0}),/0 мс/);
});
test('missing and stale registry cannot prove a schedule violation',()=>{
 assert.match(s.scheduleAssessment({Requested:{}}),/нет данных реестра/);
 assert.match(s.scheduleAssessment({ParametersDiffer:true,RegistryBeforeRequest:true,Registered:{}}),/раньше запроса/);
 assert.match(s.scheduleAssessment({ParametersDiffer:true,RegistryComplete:true,RegistryAgeMS:1,RegistryIntervalMS:10,Registered:{}}),/отличаются/);
 assert.match(s.scheduleAssessment({RegistryComplete:true,RegistryAgeMS:1,RegistryIntervalMS:10,Registered:{},Requested:{RequestedInitialDelayMS:0},Cadence:{Count:2}}),/срок.*продукта/);
});
test('timeline uses only one clock and excludes unobserved finishes',()=>{
 const row={Scope:'s',UUID:'a',Name:'x',Attempts:[{StartMS:1,EndMS:9,StartUnixMS:1000},{StartMS:2,StartUnixMS:2000,EndUnixMS:2100}]};
 const points=s.scheduleTimeline([row],false);
 assert.equal(points.length,2);assert.equal(points[0].end,points[0].at);assert.equal(points[1].end,2100);
 assert.equal(s.scheduleTimeline([{...row,Attempts:[{StartMS:1}]}],false).length,0);
});
test('bounded filtering and time formatting are explicit',()=>{
 assert.equal(s.schedulePage(Array.from({length:100},(_,i)=>({Name:'worker'+i,Scope:'s'})),'worker','s',0).rows.length,20);
 assert.match(s.scheduleTime(0,'UTC'),/1970/);
 assert.match(s.scheduleTime(null,'UTC'),/неизвест/);
 assert.match(s.scheduleDuration(90000),/1 мин 30 с/);
});
test('logical groups reduce noise without joining cadence or registry identity',()=>{
 const {scheduleGroups}=require('./templates/work-schedules.js');
 const groups=scheduleGroups([
  {UUID:'a',Scope:'s',Process:'app',Name:'config',Requested:{ObservedElapsedNanos:'100',RequestedInitialDelayMS:0}},
  {UUID:'b',Scope:'s',Process:'app',Name:'config',Requested:{ObservedElapsedNanos:'200'}},
  {UUID:'a',Scope:'other',Process:'app',Name:'config'}]);
 assert.equal(groups.length,2);assert.equal(groups[0].Rows.length,2);
 assert.equal(groups[0].Latest.UUID,'b');assert.equal(groups[1].Rows.length,1);
});
test('parameter limits explain normalization without declaring a product defect',()=>{
 const {scheduleParameterExplanation}=require('./templates/work-schedules.js');
 assert.match(scheduleParameterExplanation({RequestedPeriodMS:60000,RequestedFlexMS:1000}),/15 мин/);
 assert.match(scheduleParameterExplanation({RequestedPeriodMS:1800000,RequestedFlexMS:2100000}),/длиннее периода/);
 assert.match(scheduleParameterExplanation({RequestedPeriodMS:1800000,RequestedFlexMS:300000}),/срок.*продукта/);
 assert.match(scheduleParameterExplanation({}),/неизвест/);
});
test('closeness filter counts matching observations instead of hiding mixed pairs',()=>{
 const {scheduleFindingAtThreshold}=require('./templates/work-schedules.js');
 const f={Kind:'adjacent_starts',Count:3,MinGapMS:30000,MaxGapMS:150000,Proximity:[{LimitMS:60000,Count:1,MinGapMS:30000,MaxGapMS:30000},{LimitMS:300000,Count:3,MinGapMS:30000,MaxGapMS:150000}]};
 assert.equal(scheduleFindingAtThreshold(f,60000).Count,1);
 assert.equal(scheduleFindingAtThreshold(f,300000).Count,3);
 assert.equal(scheduleFindingAtThreshold({...f,Proximity:[]},60000),null);
});
