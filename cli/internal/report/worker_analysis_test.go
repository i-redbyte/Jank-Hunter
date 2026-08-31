package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestInspectRendersTypedWorkerLifecycleAsSeparateProblemFirstDetail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workers.html")
	summary := analyze.Summary{
		Title: "workers.jhlog",
		WorkerAnalysis: &analyze.WorkerAnalysis{
			Instances: 2, Executions: 3, Enqueued: 2, Started: 3, Finished: 2,
			Success: 1, Retries: 1, MissingEnqueue: 1, MissingFinish: 1,
			WaitSamples: 2, WaitP95MS: 1_200, WaitMaxMS: 1_500,
			RunSamples: 2, RunP95MS: 12_000, RunMaxMS: 12_000,
			MaxQueued: 2, MaxRunning: 2,
			Workers: []analyze.WorkerStats{{
				Worker: "com.app.SyncWorker", Instances: 2, Executions: 3, Enqueued: 2,
				Started: 3, Finished: 2, Success: 1, Retries: 1, MissingEnqueue: 1,
				MissingFinish: 1, WaitSamples: 2, WaitP95MS: 1_200, RunSamples: 2,
				RunP95MS: 12_000, RunMaxMS: 12_000, MaxConcurrency: 2,
				CorrelatedHTTPCalls: 4, CorrelatedHTTPFailures: 1,
				CorrelatedIOOperations: 2, CorrelatedIOBytes: 4_096,
				CPUSamples: 3, AvgDeviceCPUPercentX100: 6_250, MaxDeviceCPUPercentX100: 8_000,
				AllocationSamples: 2, AvgAllocationRateBytesPerSec: 1_048_576,
				GCCount: 2, GCTimeMS: 14, MemoryPairs: 1, AvgPSSDeltaKB: 2_048,
				MaxPSSDuringKB: 128_000,
			}},
		},
		RuntimeCalls: []analyze.RuntimeCallStats{{
			Caller: "jankhunter.semantic.v1.worker.retry.background", Callee: "com.app.SyncWorker",
			Count: 1, TotalMS: 12_000, MaxMS: 12_000,
		}},
	}
	problemReport, err := analyze.BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	summary.ProblemSchemaVersion = analyze.ProblemSchemaVersion
	summary.ProblemSummary = problemReport.Summary
	summary.Problems = problemReport.Problems
	summary.CategoryCoverage = problemReport.Coverage
	summary.Detectors = problemReport.Registry
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, expected := range []string{
		`href="#workers">Фоновые задачи`, `id="workers"`, "Жизненный цикл фоновых задач",
		"com.app.SyncWorker", "Ожидание запуска", "Параллельно работало",
		"Временная связь, а не доказанная причина", "Сетевые запросы", "Файловые операции",
		"Средняя загрузка процессора", "Изменение памяти процесса",
		"поставлено в очередь 2 · запущено 3 · завершено 2",
		"успешно 1 · ошибок 0 · повторов 1 · отмен 0",
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("worker report misses %q", expected)
		}
	}
	if strings.Contains(html, "jankhunter.semantic.v1.worker") || strings.Contains(html, "Compose · Room · Worker") {
		t.Fatal("typed worker lifecycle was duplicated by the generic semantic projection")
	}
	for _, forbidden := range []string{
		">Lifecycle<", ">Worker<", ">Lifecycle фоновых Worker<",
		"enqueue 2", "start 3", "finish 2", "retry 1",
	} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("untranslated worker term leaked into report: %q", forbidden)
		}
	}
}
