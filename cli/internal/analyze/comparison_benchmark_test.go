package analyze

import (
	"fmt"
	"testing"
)

var comparisonBenchmarkSink Comparison

func BenchmarkComparisonMaxCorpus(b *testing.B) {
	baseline, candidate := comparisonBenchmarkPair()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		comparisonBenchmarkSink = Compare(baseline, candidate)
	}
}

func TestComparisonMaxCorpusMemoryBudget(t *testing.T) {
	result := testing.Benchmark(BenchmarkComparisonMaxCorpus)
	const limit = int64(128 << 20)
	if result.AllocedBytesPerOp() > limit {
		t.Fatalf("comparison allocated %d bytes/op, limit %d", result.AllocedBytesPerOp(), limit)
	}
	baseline, candidate := comparisonBenchmarkPair()
	first, second := Compare(baseline, candidate), Compare(baseline, candidate)
	if first.Scope.Comparability != ScenarioFull || second.Scope.Comparability != first.Scope.Comparability || second.Outcome != first.Outcome || len(second.Scope.Changes) != len(first.Scope.Changes) {
		t.Fatalf("max corpus is not deterministic: first=%+v second=%+v", first.Scope, second.Scope)
	}
}

func comparisonBenchmarkPair() (Summary, Summary) {
	baselineRows := make([]comparisonCorpusRow, operationProfileLimit)
	candidateRows := make([]comparisonCorpusRow, operationProfileLimit)
	for i := range baselineRows {
		row := comparisonCorpusRow{Operation: fmt.Sprintf("operation.%04d", i), Screen: "Benchmark", Count: 20, TotalMS: 2_000, Steps: []string{"load", "render"}}
		baselineRows[i], candidateRows[i] = row, row
		candidateRows[i].TotalMS = 1_800
	}
	return corpusSummary(baselineRows, true), corpusSummary(candidateRows, false)
}
