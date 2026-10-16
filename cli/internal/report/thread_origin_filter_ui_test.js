const {test}=require('node:test');
const assert=require('node:assert/strict');
const {sourceRoots,sourceOrigin,threadOriginCategory,threadVisibleRows,threadChartCounts,threadNameLooksStandard,threadNameHint}=require('./templates/thread-snapshot.js');
const {coroutineSegmentView}=require('./templates/thread-coroutines.js');

test('thread origin requires a verified source and keeps unknown separate',()=>{
  const roots=sourceRoots('com.example.app, org.example.feature');
  assert.deepEqual(roots,['com.example.app','org.example.feature']);
  assert.equal(threadOriginCategory({name:'com.example.app.Worker'},roots),'unknown');
  assert.equal(threadOriginCategory({java:{source:'com.example.application.Other.run'}},roots),'unknown');
  assert.equal(threadOriginCategory({java:{source:'com.example.app.Worker.run'}},roots),'app');
  assert.equal(threadOriginCategory({java:{source:'android.os.HandlerThread.run'}},roots),'system');
  assert.equal(threadOriginCategory({java:{source:'kotlinx.coroutines.DefaultExecutor.run'}},roots),'library');
  assert.equal(threadOriginCategory({java:{source:'Jank Hunter writer',owned_by_jank_hunter:true}},roots),'collector');
  assert.equal(threadOriginCategory({name:'JankHunterGraph'},roots),'unknown');
  assert.deepEqual(threadVisibleRows([{tid:8,name:'JankHunterGraph'}],true),[]);
});

test('origin filters recompute rows and timeline without dropping unknown from all',()=>{
  const roots=['com.example.app'];
  const rows=[{name:'pool',tid:1},{tid:2,java:{source:'com.example.app.Task.run'}},{tid:3,java:{source:'java.lang.Thread.run'}}];
  assert.equal(threadVisibleRows(rows,false,'').length,3);
  assert.deepEqual(threadVisibleRows(rows,false,'','app',roots).map(row=>row.tid),[2]);
  assert.deepEqual(threadChartCounts([{Snapshot:{threads:rows}}],false,'','unknown',roots),[1]);
});

test('candidate view excludes common runtime names but keeps custom unknowns explicitly unknown',()=>{
  const rows=[
    {tid:1,name:'DefaultDispatch'},
    {tid:2,name:'SampleCpuWorker'},
    {tid:3,name:'binder:900_1'},
    {tid:4,name:'SampleConfirmed',java:{source:'com.example.app.Work.run'}},
  ];
  assert.equal(threadNameLooksStandard(rows[0]),true);
  assert.equal(threadOriginCategory(rows[1],['com.example.app']),'unknown');
  assert.deepEqual(threadVisibleRows(rows,false,'','app-candidates',['com.example.app']).map(row=>row.tid),[2,4]);
  assert.deepEqual(threadVisibleRows(rows,false,'','standard-names',['com.example.app']).map(row=>row.tid),[1,3]);
});

test('common Android and library names are explicit hints, never verified creator identity',()=>{
  for(const [name,subsystem] of [
    ['DefaultDispatch','Kotlin'],['OkHttp TaskRunn','OkHttp'],['ConscryptStatsL','Conscrypt'],
    ['RxCachedThreadS','RxJava'],['Firebase-Messag','Firebase'],['binder:900_1','Android Binder']]){
    const row={name};
    assert.equal(threadNameLooksStandard(row),true);
    assert.match(threadNameHint(row),new RegExp(subsystem));
    assert.equal(threadOriginCategory(row,['com.example.app']),'unknown');
  }
});

test('coroutine owner is classified independently of execution thread',()=>{
  const scope='session';
  const segments=[
    {Scope:scope,Owner:'com.example.app.Screen.load',EntryTID:'7',EntryJavaThreadID:'12'},
    {Scope:scope,Owner:'kotlinx.coroutines.DefaultExecutor',EntryTID:'7',EntryJavaThreadID:'12'},
    {Scope:scope,Owner:'unknown',EntryTID:'7',EntryJavaThreadID:'12'},
  ];
  assert.equal(sourceOrigin(segments[0].Owner,['com.example.app']),'app');
  assert.equal(coroutineSegmentView(segments,scope,'','','', '', 'app',['com.example.app']).total,1);
  assert.equal(coroutineSegmentView(segments,scope,'','',0,'','library',['com.example.app']).total,1);
  assert.equal(coroutineSegmentView(segments,scope,'','',0,'','unknown',['com.example.app']).total,1);
});
