package report

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

// Run only through scripts/concurrency-performance-test.mjs. The fixture has
// 500 thread entities, 10,000 task entities and exactly 100,000 observations.
func TestWriteCausalPerformanceFixture(t *testing.T) {
	path := os.Getenv("JH_CAUSAL_BENCH_OUT")
	if path == "" {
		t.Skip("set JH_CAUSAL_BENCH_OUT to generate the browser benchmark fixture")
	}
	analysis := &analyze.CausalAnalysis{Available: true, Complete: true, Events: 100_000, Entities: 10_500,
		AppEntities: 10_500, Coverage: "Контролируемый синтетический сценарий без пропусков.",
		Metrics: analyze.CausalMetrics{ThreadGenerations: 500, TaskSubmissions: 10_000, RunStarts: 10_500}}
	appendEntity := func(kind jhlog.CausalEntityKind, id uint64, observations int) {
		entity := analyze.CausalEntityView{Scope: "benchmark-run/process/session", Process: "app.benchmark",
			Kind: uint64(kind), ID: id, Anchor: "causal-benchmark-" + strconv.FormatUint(uint64(kind), 10) + "-" + strconv.FormatUint(id, 10),
			Source: "app.benchmark.Work#run()V", SourceLine: id%100 + 1, Responsibility: "app_direct", DirectOrigin: "PROJECT",
			ObservedSpanNS: 12_000_000, LifecycleNote: "наблюдаемый интервал"}
		if kind == jhlog.CausalExecutorTask {
			parent := (id-1)%500 + 1
			entity.ParentKind, entity.ParentID = uint64(jhlog.CausalThread), parent
			entity.ParentAnchor = "causal-benchmark-1-" + strconv.FormatUint(parent, 10)
		}
		for i := 0; i < observations; i++ {
			entity.Events = append(entity.Events, analyze.CausalEventView{Phase: 1, ObservedNS: uint64(i) * 1_000_000,
				Source: entity.Source, SourceLine: entity.SourceLine, SourceOrigin: "PROJECT",
				KnownFields: jhlog.CausalHasDurationNanos | jhlog.CausalHasCPUNanos,
				DurationNS: 1_000_000, CPUNS: 50_000})
		}
		analysis.AllEntities = append(analysis.AllEntities, entity)
	}
	for i := uint64(1); i <= 500; i++ {
		appendEntity(jhlog.CausalThread, i, 20)
	}
	for i := uint64(1); i <= 10_000; i++ {
		appendEntity(jhlog.CausalExecutorTask, i, 9)
	}
	issue := analysis.AllEntities[len(analysis.AllEntities)-1]
	analysis.Findings = []analyze.CausalFinding{{Level: "problem", Title: "Проверка позднего якоря",
		Consequence: "Измерена задержка исполнения.", Source: issue.Source, SourceLine: issue.SourceLine,
		Responsibility: "app_direct", Confidence: "синтетическая", Coverage: analysis.Coverage,
		NextAction: "Откройте причинную цепочку.", Count: 1, Anchor: issue.Anchor}}
	analysis.EntitiesForReview = append(analysis.EntitiesForReview, issue)
	analysis.EntitiesForReview = append(analysis.EntitiesForReview, analysis.AllEntities[:99]...)
	analysis.ReviewOmitted = uint64(len(analysis.AllEntities) - len(analysis.EntitiesForReview))
	started := time.Now()
	if err := WriteThreadInspectWithOptions(path, analyze.Summary{CausalAnalysis: analysis}, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("causal fixture: entities=%d observations=%d html_bytes=%d render_ms=%d",
		len(analysis.AllEntities), analysis.Events, stat.Size(), time.Since(started).Milliseconds())
}
