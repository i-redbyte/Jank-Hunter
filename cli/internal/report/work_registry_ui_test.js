const assert = require('node:assert/strict');
const {test} = require('node:test');
const {registrySnapshotRange} = require('./templates/work-registry.js');

test('long sessions expose every snapshot through bounded selector pages', () => {
  let count = 0;
  for (let page = 0; page < 63; page++) {
    const range = registrySnapshotRange(12500, page);
    assert.ok(range.end - range.start <= 200);
    count += range.end - range.start;
  }
  assert.equal(count, 12500);
  assert.deepEqual(registrySnapshotRange(0, 100), {start: 0, end: 0, page: 0, pages: 1});
  assert.deepEqual(registrySnapshotRange(201, 100), {start: 200, end: 201, page: 1, pages: 2});
});

test('job card retains only a bounded history ending at the selected observation', () => {
  const {registryJobHistory} = require('./templates/work-registry.js');
  const snapshots = Array.from({length: 12500}, (_, index) => ({Scope: 'one', ObservedElapsedMS: index, Jobs: [{UUID: 'job'}]}));
  const history = registryJobHistory(snapshots, snapshots[1200], 'job');
  assert.equal(history.total, 1201);
  assert.equal(history.observations.length, 200);
  assert.equal(history.observations[0].snapshot.ObservedElapsedMS, 1001);
  assert.equal(history.observations.at(-1).snapshot.ObservedElapsedMS, 1200);
  assert.equal(registryJobHistory(snapshots, {Scope: 'other', ObservedElapsedMS: 20000}, 'job').total, 0);
});
test('navigation selects the latest matching UUID only within the requested session',()=>{
 const {registryFindJob}=require('./templates/work-registry.js');
 const snapshots=[{Scope:'a',ObservedElapsedMS:20,Jobs:[{UUID:'u'}]},{Scope:'b',ObservedElapsedMS:30,Jobs:[{UUID:'u'}]},{Scope:'a',ObservedElapsedMS:10,Jobs:[{UUID:'u'}]}];
 assert.equal(registryFindJob(snapshots,'a','u'),0);
 assert.equal(registryFindJob(snapshots,'c','u'),-1);
});

test('initial snapshot is latest by calendar observation, not scope sort order',()=>{
 const {registryLatestSnapshotIndex}=require('./templates/work-registry.js');
 assert.equal(registryLatestSnapshotIndex([{Scope:'a',ObservedUnixMS:300},{Scope:'z',ObservedUnixMS:100}]),0);
 assert.equal(registryLatestSnapshotIndex([{ObservedUnixMS:300},{ObservedUnixMS:null,RecordedTimeMS:4000000}]),0);
 assert.equal(registryLatestSnapshotIndex([]),-1);
});

test('progress evidence retains each scoped WorkInfo observation even when metadata looks unchanged',()=>{
 const {registryJobHistory,registryProgressTimeline}=require('./templates/work-registry.js');
 const metadata=[{Key:'token',Type:'string',PayloadBytes:16}];
 const snapshots=[
  {Scope:'a',ObservedElapsedMS:100,Complete:true,Jobs:[{UUID:'u',Progress:metadata,OmittedProgressKeys:0}]},
  {Scope:'b',ObservedElapsedMS:110,Complete:true,Jobs:[{UUID:'u',Progress:[{Key:'other'}]}]},
  {Scope:'a',ObservedElapsedMS:120,Complete:false,Jobs:[{UUID:'u',Progress:metadata,OmittedProgressKeys:1}]},
 ];
 const history=registryJobHistory(snapshots,snapshots[2],'u');
 assert.deepEqual(registryProgressTimeline(history),[
  {ObservedElapsedMS:100,Complete:true,Progress:metadata,OmittedProgressKeys:0},
  {ObservedElapsedMS:120,Complete:false,Progress:metadata,OmittedProgressKeys:1},
 ]);
});
