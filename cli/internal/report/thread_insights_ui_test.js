const {test} = require('node:test');
const assert = require('node:assert/strict');
const {threadDiagnosticInsights, previousComplete} = require('./templates/thread-insights.js');

const row = (tid, name = 'AppWorker', source = 'com.app.Worker.run', owned = false) =>
  ({tid, start_ticks: 1, name, os_state: 'S', java: {source, owned_by_jank_hunter: owned}});
const snapshot = (time, rows, complete = true) => ({TimeMS: time, TimeBasis: 'observation_clock',
  Snapshot: {threads: rows, complete, elapsed_ms: time, scan_duration_ms: 0, interval_ms: 1000}});
const history = (tid, times, options = {}) => ({TID: tid, StartTicks: 1, FirstMS: times[0],
  LastMS: times.at(-1), DisappearedByMS: options.disappeared || 0,
  PresentAtObservationStart: options.atStart || false,
  Observations: times.map((time, index) => ({TimeMS: time, ElapsedMS: time,
    Row: options.rows?.[index] || row(tid), CPU: options.cpu?.[index] || null}))});

test('growth is recalculated after Jank Hunter exclusion and shows new TID route', () => {
  const own = count => Array.from({length: count}, (_, i) => row(i + 1));
  const collector = count => Array.from({length: count}, (_, i) => row(i + 100,
    'JankHunterEvent', 'io.jankhunter.runtime.Event.run', true));
  const p = {StartAtMS: 1000,
    Snapshots: [snapshot(1000, own(1).concat(collector(1))),
      snapshot(2000, own(3).concat(collector(3))),
      snapshot(3000, own(5).concat(collector(5))),
      snapshot(4000, own(7).concat(collector(7)))],
    Histories: [history(2, [2000])], Scope: 's'};
  const filtered = threadDiagnosticInsights(p, {excludeOwned: true, roots: 'com.app'});
  assert.equal(filtered.findings[0].code, 'observed_growth');
  assert.match(filtered.findings[0].evidence, /1 → 7 \(\+6\)/);
  assert.deepEqual(filtered.findings[0].related.map(item => item.tid), [2, 3, 4, 5, 6, 7]);
  const collectorOnly = {Snapshots: [snapshot(1000, collector(1)), snapshot(2000, collector(3)),
    snapshot(3000, collector(5)), snapshot(4000, collector(7))], Histories: []};
  assert.deepEqual(threadDiagnosticInsights(collectorOnly, {excludeOwned: true}).findings, []);
  assert.equal(threadDiagnosticInsights(collectorOnly, {excludeOwned: false}).findings[0].code,
    'observed_growth');
});

test('new Java attribution on unchanged TID generations is not thread growth', () => {
  const selected = (time, confirmed) => ({TimeMS: time, TimeBasis: 'observation_clock',
    Snapshot: {complete: true, elapsed_ms: time, scan_duration_ms: 0, interval_ms: 1000,
      threads: Array.from({length: 7}, (_, i) => ({tid: i + 1, start_ticks: 9,
        name: 'worker-' + i, ...(i < confirmed ? {java: {source: 'com.app.Worker.run'}} : {})}))}});
  const p = {Scope: 'same-session', Snapshots: [selected(1000, 1), selected(2000, 3),
    selected(3000, 5), selected(4000, 7)], Histories: []};
  const result = threadDiagnosticInsights(p, {origin: 'app', roots: 'com.app', excludeOwned: true});
  assert.equal(result.findings.some(item => item.code === 'observed_growth' ||
    item.code === 'observed_burst'), false);
});

test('incomplete snapshots do not prove growth or burst', () => {
  const many = n => Array.from({length: n}, (_, i) => row(i + 1));
  const p = {Snapshots: [snapshot(1000, many(1), false), snapshot(2000, many(10))], Histories: []};
  assert.deepEqual(threadDiagnosticInsights(p).findings, []);
  assert.equal(threadDiagnosticInsights(p).coverage.partialSnapshots, 1);
});

test('missing Java source leaves origin coverage explicitly unknown', () => {
  const unknown = {tid: 3, start_ticks: 1, name: 'AppWorker', os_state: 'S'};
  const p = {Snapshots: [snapshot(1000, [unknown])], Histories: [
    {TID: 3, StartTicks: 1, FirstMS: 1000, LastMS: 1000,
      Observations: [{TimeMS: 1000, ElapsedMS: 1000, Row: unknown}]}
  ]};
  const result = threadDiagnosticInsights(p, {excludeOwned: true});
  assert.equal(result.coverage.observedGenerations, 1);
  assert.equal(result.coverage.knownOriginGenerations, 0);
  assert.equal(result.coverage.sourceObservedGenerations, 0);
});

test('unrecognized application namespace is suggested without automatic attribution', () => {
  const p = {Snapshots: [snapshot(1000, [row(5, 'Worker', 'ru.mail.im.requests.Worker.run')])],
    Histories: [history(5, [1000], {rows: [row(5, 'Worker', 'ru.mail.im.requests.Worker.run')]})]};
  const result = threadDiagnosticInsights(p, {roots: 'ru.mail.biz.avocado'});
  assert.equal(result.coverage.knownOriginGenerations, 0);
  assert.equal(result.coverage.sourceObservedGenerations, 1);
  assert.equal(result.coverage.unclassifiedSourceGenerations, 1);
  assert.deepEqual(result.coverage.unclassifiedSourceHints, ['ru.mail.im']);
});

test('CPU and OS waiting findings use only selected thread generations', () => {
  const app = history(7, [1000, 3000], {cpu: [null,
    {ElapsedMS: 2000, CPUTimeMS: 1800, CorePercent: 90}]});
  const wait = history(8, [1000, 2000, 3000], {rows: [1000, 2000, 3000].map(() =>
    ({...row(8), os_state: 'D'}))});
  const own = history(9, [1000, 3000], {rows: [1000, 3000].map(() =>
    row(9, 'JankHunterEvent', 'io.jankhunter.runtime.Event.run', true)),
  cpu: [null, {ElapsedMS: 2000, CPUTimeMS: 2000, CorePercent: 100}]});
  const p = {Snapshots: [snapshot(1000, [row(7), row(8)]),
    snapshot(3000, [row(7), row(8)])], Histories: [app, wait, own]};
  const result = threadDiagnosticInsights(p, {excludeOwned: true, roots: 'com.app'});
  assert.deepEqual(result.findings.map(item => item.code), ['measured_cpu', 'repeated_os_wait']);
  assert.deepEqual(result.findings.map(item => item.tid), [7, 8]);
  assert.deepEqual(result.coverage, {snapshots: 2, completeSnapshots: 2, partialSnapshots: 0,
    droppedSnapshots: 0, observedGenerations: 2, knownOriginGenerations: 2,
    sourceObservedGenerations: 2, unclassifiedSourceGenerations: 0, unclassifiedSourceHints: [],
    cpuIntervals: 1, selectedCPUms: 1800});
});

test('short generation signal needs complete lifetime brackets and repeated evidence', () => {
  const rows = Array.from({length: 8}, (_, i) => row(i + 1));
  const histories = rows.map(r => history(r.tid, [5000], {disappeared: 10000}));
  const p = {Snapshots: [snapshot(0, []), snapshot(5000, rows), snapshot(10000, [])],
    Histories: histories};
  const signal = threadDiagnosticInsights(p).findings.find(item => item.code === 'short_generations');
  assert.ok(signal);
  assert.equal(signal.related.length, 8);
  const noPriorComplete = {...p, Snapshots: [snapshot(0, [], false),
    snapshot(5000, rows), snapshot(10000, [])]};
  assert.equal(threadDiagnosticInsights(noPriorComplete).findings.some(item =>
    item.code === 'short_generations'), false);
});

test('record-time wall-clock shift cannot make a long monotonic lifetime look short', () => {
  const rows = Array.from({length: 8}, (_, i) => row(i + 1));
  const point = (wall, monotonic, values) => ({TimeMS: wall, TimeBasis: 'record_time',
    Snapshot: {threads: values, complete: true, elapsed_ms: monotonic,
      scan_duration_ms: 100, interval_ms: 1000}});
  const p = {Snapshots: [point(100000, 1000, []), point(100500, 5000, rows),
    point(100600, 30000, [])], Histories: rows.map(r => ({TID: r.tid, StartTicks: 1,
      FirstMS: 100500, LastMS: 100500, DisappearedByMS: 100600,
      Observations: [{TimeMS: 100500, ElapsedMS: 5000, Row: r}]}))};
  assert.equal(threadDiagnosticInsights(p).findings.some(item => item.code === 'short_generations'), false);
  // A partial snapshot cannot provide the preceding lifetime boundary either.
  p.Snapshots[0].Snapshot.complete = false;
  p.Snapshots[2].Snapshot.elapsed_ms = 10000;
  assert.equal(threadDiagnosticInsights(p).findings.some(item => item.code === 'short_generations'), false);
});

test('late Jank Hunter ownership excludes the entire generation from diagnostics', () => {
  const early = {tid: 77, start_ticks: 9, name: 'EventThread', os_state: 'S'};
  const late = {...early, java: {owned_by_jank_hunter: true, source: 'Jank Hunter Event'}};
  const p = {Snapshots: [snapshot(1000, [early]), snapshot(3000, [late])],
    Histories: [{TID: 77, StartTicks: 9, FirstMS: 1000, LastMS: 3000,
      Observations: [{TimeMS: 1000, ElapsedMS: 1000, Row: early},
        {TimeMS: 3000, ElapsedMS: 3000, Row: late,
          CPU: {ElapsedMS: 2000, CPUTimeMS: 1800, CorePercent: 90}}]}]};
  const withoutCollector = threadDiagnosticInsights(p, {excludeOwned: true});
  assert.equal(withoutCollector.coverage.observedGenerations, 0);
  assert.equal(withoutCollector.coverage.cpuIntervals, 0);
  assert.deepEqual(withoutCollector.findings, []);
  assert.equal(threadDiagnosticInsights(p, {excludeOwned: false}).findings[0].code, 'measured_cpu');
});

test('previous complete snapshot lookup stays logarithmic for large histories', () => {
  const points = Array.from({length: 65536}, (_, i) => ({TimeMS: i * 1000}));
  let reads = 0;
  const tracked = new Proxy(points, {get(target, key, receiver) {
    if (typeof key === 'string' && /^\d+$/.test(key)) reads++;
    return Reflect.get(target, key, receiver);
  }});
  assert.equal(previousComplete(tracked, 32000500).TimeMS, 32000000);
  assert.ok(reads <= 18, `expected logarithmic lookup, got ${reads} point reads`);
});
