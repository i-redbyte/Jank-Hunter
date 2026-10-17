const {test} = require('node:test');
const assert = require('node:assert/strict');
const {threadOverviewIndex, threadOverviewView} = require('./templates/thread-overview.js');

const row = (name, tid, source) => ({name, tid, start_ticks: 1, ...(source ? {java: {source}} : {})});
const history = (tid, name, points, end = 0) => ({TID: tid, StartTicks: 1, FirstMS: points[0].TimeMS,
  LastMS: points.at(-1).TimeMS, DisappearedByMS: end, Observations: points.map(point =>
    ({...point, Row: row(name, tid, point.source)}))});

test('overview reports observed generations, peak, measured CPU and completion TIDs', () => {
  const p = {Scope: 's', StartAtMS: 1000, Histories: [
    history(7, 'AppWorker', [{TimeMS: 1000}, {TimeMS: 2000, CPU: {CPUTimeMS: 250}}], 3000),
    history(8, 'binder:123', [{TimeMS: 1000}, {TimeMS: 2000, CPU: null}])],
    Snapshots: [{Snapshot: {threads: [row('AppWorker', 7), row('binder:123', 8)]}},
      {Snapshot: {threads: [row('AppWorker', 7), row('binder:123', 8)]}}]};
  const data = {Processes: [p], Resources: [{Scope: 's', CompletionTID: '7', Kind: 'database'},
    {Scope: 'other', CompletionTID: '7', Kind: 'http'}],
    CoroutineSegments: [{Scope: 's', EntryTID: '7', ExitTID: '7'}]};
  const index = threadOverviewIndex(data);
  const all = threadOverviewView(p, index, {roots: 'com.app'});
  assert.deepEqual([all.rows.length, all.peak, all.cpuMS, all.cpuSamples, all.operationCount,
    all.coroutineTIDs, all.disappeared], [2, 2, 250, 1, 1, 1, 1]);
  assert.equal(all.noEvidenceCount, 0);
  const own = threadOverviewView(p, index, {origin: 'app-candidates', roots: 'com.app'});
  assert.equal(own.rows.length, 1);
  assert.equal(own.peak, 1);
  assert.equal(own.rows[0].origin, 'unknown');
  assert.equal(threadOverviewView(p, index, {search: 'binder', roots: 'com.app'}).operationCount, 0);
});

test('reused TID does not attribute operations or coroutines to either generation', () => {
  const p = {Scope: 's', StartAtMS: 1000, Histories: [
    history(7, 'first', [{TimeMS: 1000}]), history(7, 'second', [{TimeMS: 3000}])],
    Snapshots: [{Snapshot: {threads: [row('first', 7)]}}, {Snapshot: {threads: [row('second', 7)]}}]};
  const view = threadOverviewView(p, threadOverviewIndex({Processes: [p],
    Resources: [{Scope: 's', CompletionTID: '7'}],
    CoroutineSegments: [{Scope: 's', EntryTID: '7'}]}), {});
  assert.equal(view.rows.length, 2);
  assert.equal(view.operationCount, 0);
  assert.equal(view.coroutineTIDs, 0);
  assert.ok(view.rows.every(item => item.tidReused));
  assert.ok(view.rows.every(item => !item.resources && !item.coroutines));
  assert.ok(view.rows.every(item => item.cpuSamples === 0));
});

test('late Java source does not add earlier CPU intervals to filtered view', () => {
  const unknown = row('Worker', 7);
  const app = row('Worker', 7, 'com.app.Worker.run');
  const p = {Scope: 's', Histories: [{TID: 7, StartTicks: 1,
    Observations: [{TimeMS: 1000, Row: unknown},
      {TimeMS: 2000, Row: unknown, CPU: {CPUTimeMS: 900}},
      {TimeMS: 3000, Row: app, CPU: {CPUTimeMS: 800}},
      {TimeMS: 4000, Row: app, CPU: {CPUTimeMS: 700}}]}],
    Snapshots: [{Snapshot: {threads: [unknown]}}, {Snapshot: {threads: [unknown]}},
      {Snapshot: {threads: [app]}}, {Snapshot: {threads: [app]}}]};
  const view = threadOverviewView(p, threadOverviewIndex({Processes: [p]}),
    {origin: 'app', roots: 'com.app'});
  assert.equal(view.peak, 1);
  assert.equal(view.cpuMS, 700);
  assert.equal(view.cpuSamples, 1);
});

test('start caller requires a unique scoped Java ID even if start returns after the final snapshot', () => {
  const p = {Scope: 's', StartAtMS: 1000, Histories: [{TID: 7, StartTicks: 1, FirstMS: 1000,
    LastMS: 2000, Observations: [{TimeMS: 1000, ElapsedMS: 10, Row: {name: 'worker', tid: 7, java: {id: '99'}}},
      {TimeMS: 2000, ElapsedMS: 20, Row: {name: 'worker', tid: 7, java: {id: '99'}}}]}],
    Snapshots: [{Snapshot: {threads: [{name: 'worker', tid: 7}]}}]};
  const start = (scope, at) => ({Scope: scope, Observation: {stage: 2, java_id: '99',
    observed_elapsed_nanos: at, source: 'com.app.Caller.launch'}});
  const view = lifecycle => threadOverviewView(p, threadOverviewIndex({Processes: [p], Lifecycle: lifecycle}), {}).rows[0];
  assert.equal(view([start('other', '15000000')]).startSource, '');
  assert.equal(view([start('s', '21000000')]).startSource, 'com.app.Caller.launch');
  assert.equal(view([start('s', '21000000')]).startReturnedAfterLast, true);
  assert.equal(view([start('s', '15000000')]).startSource, 'com.app.Caller.launch');
  assert.equal(view([start('s', '15000000'), start('s', '16000000')]).startAmbiguous, true);
});

test('factory return, observed run and activity remain separate evidence', () => {
  const thread = {name: 'Wim-Out-1', tid: 7, start_ticks: 1,
    java: {id: '99', source: 'com.app.Work.run'}};
  const p = {Scope: 's', StartAtMS: 1000, Histories: [{TID: 7, StartTicks: 1, FirstMS: 1000,
    LastMS: 2000, Observations: [{ElapsedMS: 10, Row: thread}, {ElapsedMS: 20, Row: thread}]}],
  Snapshots: [{Snapshot: {threads: [thread]}}]};
  const factory = (scope, at) => ({Scope: scope, Observation: {stage: 3, java_id: '99',
    observed_elapsed_nanos: at, source: 'com.app.Pool.newThread', factory: 'com.app.PoolFactory'}});
  const view = lifecycle => threadOverviewView(p, threadOverviewIndex({Processes: [p], Lifecycle: lifecycle,
    Resources: [{Scope: 's', CompletionTID: '7', Kind: 'database', Source: 'com.app.Query.read'}],
    CoroutineSegments: [{Scope: 's', EntryTID: '7', Owner: 'com.app.Loader.load'}]}), {});
  const matched = view([factory('s', '15000000')]);
  assert.equal(matched.factoryKnown, 1);
  assert.equal(matched.executionSourceKnown, 1);
  assert.equal(matched.startKnown, 0);
  assert.equal(matched.noEvidenceCount, 0);
  assert.equal(matched.rows[0].factoryName, 'com.app.PoolFactory');
  assert.equal(matched.rows[0].source, 'com.app.Work.run');
  assert.deepEqual(matched.rows[0].resources.examples, ['com.app.Query.read']);
  assert.deepEqual(matched.rows[0].coroutines.examples, ['com.app.Loader.load']);
  assert.equal(view([factory('other', '15000000')]).rows[0].factorySource, '');
  assert.equal(view([factory('s', '21000000')]).rows[0].factorySource, 'com.app.Pool.newThread');
  assert.equal(view([factory('s', '21000000')]).rows[0].factoryReturnedAfterLast, true);
  assert.equal(view([factory('s', '15000000'), factory('s', '16000000')]).rows[0].factoryAmbiguous, true);
});

test('reused Java ID or conflicting Java identities prevent lifecycle attribution', () => {
  const observation = (tid, javaID) => ({ElapsedMS: 10, Row: {name: 'worker', tid,
    start_ticks: 1, java: {id: javaID}}});
  const first = {TID: 7, StartTicks: 1, Observations: [observation(7, '99')]};
  const reused = {TID: 8, StartTicks: 1, Observations: [observation(8, '99')]};
  const conflicted = {TID: 9, StartTicks: 1, Observations: [observation(9, '100'), observation(9, '101')]};
  const p = {Scope: 's', Histories: [first, reused, conflicted], Snapshots: []};
  const event = (id, stage) => ({Scope: 's', Observation: {stage, java_id: id,
    source: 'com.app.Factory.call', factory: 'com.app.Factory'}});
  const view = threadOverviewView(p, threadOverviewIndex({Processes: [p], Lifecycle: [
    event('99', 2), event('99', 3), event('100', 2)]}), {});
  assert.equal(view.startKnown, 0);
  assert.equal(view.factoryKnown, 0);
  assert.ok(view.rows.every(item => item.javaIdentityAmbiguous));
});

test('checked collector filter removes JH cards and CPU from the same generation', () => {
  const first = row('unmarked', 10);
  const later = {...first, java: {owned_by_jank_hunter: true}};
  const app = row('AppWorker', 11);
  const p = {Scope: 's', StartAtMS: 1000, Histories: [
    {TID: 10, StartTicks: 1, FirstMS: 1000, LastMS: 2000, Observations: [
      {TimeMS: 1000, Row: first, CPU: {CPUTimeMS: 50}},
      {TimeMS: 2000, Row: later, CPU: {CPUTimeMS: 75}}]},
    {TID: 11, StartTicks: 1, FirstMS: 1000, LastMS: 2000, Observations: [
      {TimeMS: 1000, Row: app},
      {TimeMS: 2000, Row: app, CPU: {CPUTimeMS: 20}}]},
    history(12, 'JankHunterGraph', [{TimeMS: 1000, CPU: {CPUTimeMS: 30}}])],
    Snapshots: [{Snapshot: {threads: [first, app, row('JankHunterGraph', 12)]}},
      {Snapshot: {threads: [later, app]}}]};
  const index = threadOverviewIndex({Processes: [p]});
  const all = threadOverviewView(p, index, {roots: 'com.app'});
  const filtered = threadOverviewView(p, index, {roots: 'com.app', excludeOwned: true});
  assert.deepEqual([all.rows.length, all.peak, all.cpuMS], [3, 3, 95]);
  assert.deepEqual([filtered.rows.length, filtered.peak, filtered.cpuMS], [1, 1, 20]);
  assert.equal(filtered.rows[0].row.name, 'AppWorker');
});

test('calendar rollback keeps plot bounds and observed duration valid', () => {
  const {threadTimeDomain, threadObservedDuration} = require('./templates/thread-snapshot.js');
  assert.deepEqual(threadTimeDomain({StartAtMS:1000, LastAtMS:900,
    Snapshots:[{TimeMS:1000},{TimeMS:900}]}), {first:900,end:1000,span:100});
  assert.equal(threadObservedDuration({FirstMS:1000,LastMS:900,
    Observations:[{ElapsedMS:100},{ElapsedMS:200}]}),100);
  assert.equal(threadObservedDuration({FirstMS:1000,LastMS:900,Observations:[]}),null);
});
