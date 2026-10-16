// Acceptance budgets from specs/009-concurrency-diagnostics SC-001 and SC-005.
export function validateConcurrencyPerformance(result) {
  const limits = {firstFindingMs: 5000, scopeP95Ms: 200, pageP95Ms: 200, issueP95Ms: 200, searchVisibleP95Ms: 200};
  for (const [metric, limit] of Object.entries(limits)) {
    const value = result[metric];
    if (!Number.isFinite(value) || value < 0 || value > limit) {
      throw new Error(`${metric}=${value}ms exceeds or cannot prove budget ${limit}ms`);
    }
  }
}
