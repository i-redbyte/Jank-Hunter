const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const {threadOwned,threadCollectorNameHint,threadCollectorGenerations,threadOriginCategory,threadVisibleRows,threadChartCounts,threadLifelineView,threadInScope,threadSearchMatches,threadSelectedCPUText,threadJavaText,threadOwnershipText}=require('./templates/thread-snapshot.js');
const rows=[{tid:1,name:'JankHunterFake'}, {tid:2,java:{owned_by_jank_hunter:true}}, {tid:3,java:{owned_by_jank_hunter:false}}];
test('initial view chooses the process session with the most complete thread snapshots',()=>{
 const {threadDefaultProcessIndex}=require('./templates/thread-snapshot.js');
 const snapshot=complete=>({Snapshot:{complete}});
 assert.equal(threadDefaultProcessIndex([
  {Snapshots:[snapshot(true)]},
  {Snapshots:[snapshot(true),snapshot(false),snapshot(true)]},
  {Snapshots:[snapshot(false),snapshot(false),snapshot(false),snapshot(false)]},
 ]),1);
 assert.equal(threadDefaultProcessIndex([]),0);
});
test('ownership uses explicit field, never thread name',()=>{
 assert.equal(threadOwned(rows[0]),false);
 assert.equal(threadOwned(rows[1]),true);
 assert.match(threadJavaText({id:'7',name:'maintenance',state:'WAITING',daemon:true,first_observed_elapsed_nanos:'1',source:'internal',owned_by_jank_hunter:true}),/регистрация выполняющегося потока/);
});
test('excluding owned rows recomputes the chart counts and keeps unknown rows',()=>{
 const snapshots=[{Snapshot:{threads:rows}},{Snapshot:{threads:[rows[1]]}}];
 assert.deepEqual(threadChartCounts(snapshots,true),[2,0]);
 assert.deepEqual(threadChartCounts(snapshots,false),[3,1]);
 assert.deepEqual(threadVisibleRows(rows,true),[rows[0],rows[2]]);
});
test('exact internal names and Linux-truncated names are hidden without claiming confirmed ownership',()=>{
 const graph={tid:4,name:'JankHunterGraph'};
 const event={tid:5,name:'JankHunterEvent'};
 const app={tid:6,name:'JankHunterGraphWorker'};
 const explicitApp={tid:7,name:'JankHunterGraph',java:{owned_by_jank_hunter:false}};
 assert.equal(threadOwned(graph),false);
 assert.equal(threadCollectorNameHint(graph),true);
 assert.equal(threadCollectorNameHint(event),true);
 assert.equal(threadCollectorNameHint(app),false);
 assert.equal(threadCollectorNameHint(explicitApp),false);
 assert.match(threadOwnershipText(graph),/предположен по известному имени/);
 assert.deepEqual(threadVisibleRows([graph,event,app,explicitApp],true),[app,explicitApp]);
 assert.deepEqual(threadChartCounts([{Snapshot:{threads:[graph,event,app,explicitApp]}}],true),[2]);
});
test('confirmed ownership in one observation hides only that TID generation',()=>{
 const early={tid:10,start_ticks:11,name:'worker'};
 const late={...early,java:{owned_by_jank_hunter:true}};
 const reused={...early,start_ticks:12};
 const process={Histories:[
  {TID:10,StartTicks:11,Observations:[{Row:early},{Row:late}]},
  {TID:10,StartTicks:12,Observations:[{Row:reused}]}
 ]};
 const generations=threadCollectorGenerations(process);
 assert.equal(generations.get('10:11'),'confirmed');
 assert.equal(threadOriginCategory(early, [], generations),'collector');
 assert.deepEqual(threadVisibleRows([early,reused],true,'','',[],generations),[reused]);
 assert.deepEqual(threadChartCounts([{Snapshot:{threads:[early,reused]}}],true,'','',[],generations),[1]);
});
test('name-only generation is hidden but remains unknown in origin analysis',()=>{
 const guessed={tid:20,start_ticks:8,name:'JankHunterEvent'};
 const process={Histories:[{TID:20,StartTicks:8,Observations:[{Row:guessed}]}]};
 const generations=threadCollectorGenerations(process);
 assert.equal(generations.get('20:8'),'hint');
 assert.equal(threadOriginCategory(guessed, [], generations),'unknown');
 assert.deepEqual(threadVisibleRows([guessed],true,'','',[],generations),[]);
});
test('source filter recomputes chart scope from verified Java source only',()=>{
 const {threadChartCounts,threadVisibleRows}=require('./templates/thread-snapshot.js');
 const observations=[{Snapshot:{threads:[
  {tid:1,name:'pool-Task.run'},
  {tid:2,java:{source:'app.Task.run',owned_by_jank_hunter:false}},
  {tid:3,java:{source:'Jank Hunter maintenance',owned_by_jank_hunter:true}},
 ]}}];
 assert.deepEqual(threadChartCounts(observations,false,'task.run'),[1]);
 assert.deepEqual(threadVisibleRows(observations[0].Snapshot.threads,false,'task.run').map(r=>r.tid),[2]);
 assert.deepEqual(threadChartCounts(observations,true,'maintenance'),[0]);
});
test('lifeline grouping uses an observed source matching the selected filter',()=>{
 const {threadHistorySource}=require('./templates/thread-snapshot.js');
 const history={Observations:[{Row:{name:'x'}},{Row:{java:{source:'app.First.run'}}},{Row:{java:{source:'app.Second.run'}}}]};
 assert.equal(threadHistorySource(history,''),'app.First.run');
 assert.equal(threadHistorySource(history,'second'),'app.Second.run');
 assert.equal(threadHistorySource(history,'missing'),'');
});
test('one upper search narrows chart snapshots and keeps only matching lifeline runs',()=>{
 const bare={tid:10,start_ticks:1,name:'Worker'};
 const app={...bare,java:{source:'com.app.Work.run'}};
 const other={tid:11,start_ticks:1,name:'Other'};
 const snapshots=[{Snapshot:{threads:[bare,other]}},{Snapshot:{threads:[app,other]}},
  {Snapshot:{threads:[bare,other]}},{Snapshot:{threads:[app,other]}}];
 const selected=row=>threadInScope(row,true,'','app',['com.app']) && threadSearchMatches(row,'10');
 assert.deepEqual(threadChartCounts(snapshots,true,'','app',['com.app'],undefined,'10'),[0,1,0,1]);
 const histories=[{TID:10,Observations:[bare,app,bare,app].map((Row,i)=>({Row,TimeMS:i*1000}))},
  {TID:11,Observations:[{Row:other,TimeMS:0}]}];
 const lines=threadLifelineView(histories,selected);
 assert.equal(lines.length,1);
 assert.deepEqual(lines[0].points.map(point=>point.TimeMS),[1000,3000]);
 assert.deepEqual(lines[0].runs.map(run=>run.map(point=>point.TimeMS)),[[1000],[3000]]);
});
test('raw CPU delta distinguishes a prior observation outside the selected area',()=>{
 const cpu={CorePercent:80,ElapsedMS:1000,TickMS:10};
 assert.match(threadSelectedCPUText(cpu,true,false),/не вошла в сводку: предыдущее наблюдение вне фильтра/);
 assert.doesNotMatch(threadSelectedCPUText(cpu,true,true),/не вошла в сводку/);
 assert.equal(threadSelectedCPUText(null,true,false),'неизвестно');
});
test('thread page has one shared name and TID search and a confirmed-only collector origin',()=>{
 const html=fs.readFileSync(require.resolve('./templates/inspect.tmpl'),'utf8');
 assert.equal((html.match(/data-thread-overview-search/g)||[]).length,1);
 assert.doesNotMatch(html,/data-thread-search/);
 assert.match(html,/<option value="collector">Jank Hunter: подтверждён<\/option>/);
});
