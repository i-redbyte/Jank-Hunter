const test = require('node:test');
const assert = require('node:assert/strict');
const browser = require('./templates/concurrency-browser.js');

test('causal browser keeps app focus and makes internal work explicit', () => {
  const entities = [
    {ID: 1, Kind: 2, Source: 'app.Home#load()V', Responsibility: 'app_direct'},
    {ID: 2, Kind: 7, Source: 'library.Store#read()V', AppCaller: 'app.Home#load()V', Responsibility: 'library_initiated_by_app'},
    {ID: 3, Kind: 1, Source: 'unknown', Responsibility: 'unknown'},
    {ID: 4, Kind: 1, Source: 'io.jankhunter.Scan#run()V', Responsibility: 'unknown', IsJankHunter: true}
  ];
  assert.deepEqual(browser.causalFilter(entities, '', 'app').map(item => item.ID), [1, 2]);
  assert.deepEqual(browser.causalFilter(entities, '', 'all').map(item => item.ID), [1, 2, 3]);
  assert.deepEqual(browser.causalFilter(entities, '', 'unknown').map(item => item.ID), [3]);
  assert.deepEqual(browser.causalFilter(entities, '', 'jh').map(item => item.ID), [4]);
  assert.deepEqual(browser.causalFilter(entities, 'store', 'app').map(item => item.ID), [2]);
});

test('causal browser pages twenty thousand entities without prebuilding DOM cards', () => {
  const entities = Array.from({length: 20_000}, (_, index) => ({ID: index + 1, Responsibility: 'app_direct'}));
  const page = browser.causalPage(entities, 999, 20);
  assert.equal(page.rows.length, 20);
  assert.equal(page.rows[0].ID, 19_981);
  assert.equal(page.total, 1000);
  assert.equal(browser.causalPage(entities, 10_000, 20).page, 999);
});

test('known zero and unavailable cost are distinct; phase names do not claim completion from requests', () => {
  assert.equal(browser.cost(64, 64, 0, 'байт'), '0 байт');
  assert.equal(browser.cost(64, 64, undefined, 'байт'), '0 байт');
  assert.equal(browser.cost(0, 64, 0, 'байт'), 'не измерено');
  assert.equal(browser.phase(8, 3), 'Запрошена отмена серии');
  assert.equal(browser.phase(5, 3), 'Возвращён retry');
  assert.equal(browser.phase(9, 7), 'shutdown выбросил исключение');
  assert.equal(browser.phase(6, 7), 'Передан в use; приобретение и открытость неизвестны');
  assert.equal(browser.phase(4, 6), 'Впервые наблюдалась корутина; создание неизвестно');
  assert.equal(browser.eventPhase(4, {phase: 4, CallResult: 0}), 'Запрошена отмена');
  assert.equal(browser.eventPhase(4, {phase: 4, CallResult: 1}), 'Вызов отмены вернулся');
  assert.equal(browser.eventPhase(4, {phase: 4, CallResult: 3}), 'Вызов отмены выбросил исключение');
});

test('timeline preserves exact relative nanoseconds and only connects observed start/end pairs', () => {
  const base = 18_000_000_000_000_001n;
  const entity = {Kind: 4, ObservedSpanNS: '5000000', Events: [
    {phase: 3, observed_ns: String(base + 5_000_000n), relative_ns: '5000000', target_tid: 8},
    {phase: 2, observed_ns: String(base), relative_ns: '0', target_tid: 8},
    {phase: 2, observed_ns: String(base + 2_000_000n), relative_ns: '2000000', target_tid: 9},
    {phase: 3, observed_ns: String(base + 3_000_000n), relative_ns: '3000000', target_tid: 9}
  ]};
  const timeline = browser.causalTimeline(entity);
  assert.deepEqual(timeline.points.map(point => point.offset), [0n, 2_000_000n, 3_000_000n, 5_000_000n]);
  assert.equal(timeline.segments.length, 1);
  assert.equal(timeline.segments[0].duration, 1_000_000n);
  assert.equal(timeline.segments[0].tid, 9);
  assert.equal(timeline.position(2_000_000n), 40);
  assert.equal(browser.relativeTime(timeline.segments[0].end.offset), '3.000 мс');
  assert.equal(browser.causalTimeline({Kind: 4, Events: [{phase: 2, observed_ns: '1'}]}).segments.length, 0);
});

test('64-bit relation keys retain distinct entity identities across the JavaScript precision boundary', () => {
  const entities = JSON.parse('[{"ID":"9007199254740993","ParentID":"9007199254740994","Kind":2},{"ID":"9007199254740994","Kind":1}]');
  const first = browser.relationKey('run/process/session', entities[0].Kind, entities[0].ID);
  const parent = browser.relationKey('run/process/session', 1, entities[0].ParentID);
  assert.notEqual(first, parent);
  assert.equal(parent, browser.relationKey('run/process/session', entities[1].Kind, entities[1].ID));
  assert.deepEqual(browser.causalFilter(entities, '9007199254740993', 'all').map(entity => entity.ID), [entities[0].ID]);
});

test('known-zero Worker attempts and coroutine generations remain zero after JSON omitempty', () => {
  assert.equal(browser.workerBindingLabel({WorkerInstanceID: '18364758544493064720'}),
    'Worker instance 18364758544493064720 · попытка 0 · поколение 0');
  assert.equal(browser.coroutineBindingLabel({CoroutineToken: '18364758544493064720'}),
    'Корутина token 18364758544493064720 · поколение 0');
});

test('resource use to close is an observed pair, without an inferred acquisition event', () => {
  const timeline = browser.causalTimeline({Kind: 6, ObservedSpanNS: '10', Events: [
    {phase: 7, observed_ns: '100', relative_ns: '0', target_tid: 5},
    {phase: 4, observed_ns: '110', relative_ns: '10', target_tid: 5}
  ]});
  assert.equal(timeline.segments.length, 1);
  assert.equal(timeline.segments[0].start.event.phase, 7);
  assert.equal(timeline.segments[0].end.event.phase, 4);
  assert.equal(browser.phase(6, 7), 'Передан в use; приобретение и открытость неизвестны');
});
