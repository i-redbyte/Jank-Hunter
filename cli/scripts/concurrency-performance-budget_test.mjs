import {test} from 'node:test';
import assert from 'node:assert/strict';
import {validateConcurrencyPerformance} from './concurrency-performance-budget.mjs';
const valid = {firstFindingMs: 5000, scopeP95Ms: 200, pageP95Ms: 200, issueP95Ms: 200, searchVisibleP95Ms: 200};
test('SC-001 and SC-005 enforce every declared budget', () => {
  assert.doesNotThrow(() => validateConcurrencyPerformance(valid));
  for (const key of Object.keys(valid)) {
    assert.throws(() => validateConcurrencyPerformance({...valid, [key]: valid[key] + 1}), new RegExp(key));
    assert.throws(() => validateConcurrencyPerformance({...valid, [key]: NaN}), new RegExp(key));
    const missing = {...valid}; delete missing[key];
    assert.throws(() => validateConcurrencyPerformance(missing), new RegExp(key));
  }
});
