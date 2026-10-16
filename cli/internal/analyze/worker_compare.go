package analyze

import (
	"sort"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

type WorkerDiagnosticComparison struct {
	Lifecycle      []WorkerDiagnosticMetric
	Catalog        WorkerCatalogComparison
	Registry       []WorkerRegistryComparison
	Classes        []WorkerClassComparison
	OmittedClasses uint64
	Note           string
}
type WorkerClassComparison struct {
	Class                           string
	BaselineStarts, CandidateStarts uint64
	BaselineKnown, CandidateKnown   bool
	DeltaStarts                     *float64
	Comparable                      bool
}
type WorkerDiagnosticMetric struct {
	Key, Label, Unit           string
	Baseline, Candidate, Delta *float64
	Comparable                 bool
}

func compareWorkerDiagnostics(before, after Summary, scope ComparisonScope) WorkerDiagnosticComparison {
	compatible := workerComparisonCoverage(before, after, scope)
	result := WorkerDiagnosticComparison{Note: "Сравниваются захваченные наблюдения. Retry, остановка или изменение числа задач сами по себе не являются проблемой. UUID не сопоставляет задания разных запусков. При неизвестном сценарии, охвате или потерях численная дельта недоступна. Различающиеся или смешанные режимы наблюдения не дают сопоставимой диагностической дельты; у старых журналов режим не заявлен."}
	type metric struct {
		key, label, unit string
		value            func(*WorkerAnalysis) uint64
		known            func(*WorkerAnalysis) bool
		rate             bool
	}
	known := func(*WorkerAnalysis) bool { return true }
	metrics := []metric{
		{"starts", "Наблюдаемые start", "шт", func(a *WorkerAnalysis) uint64 { return a.Started }, known, false},
		{"starts_per_minute", "Start на минуту записанного окна", "шт/мин", func(a *WorkerAnalysis) uint64 { return a.Started }, known, true},
		{"success", "Успехи", "шт", func(a *WorkerAnalysis) uint64 { return a.Success }, known, false},
		{"failures", "Ошибки", "шт", func(a *WorkerAnalysis) uint64 { return a.Failures }, known, false},
		{"retries", "Retry", "шт", func(a *WorkerAnalysis) uint64 { return a.Retries }, known, false},
		{"cancelled", "Отмены", "шт", func(a *WorkerAnalysis) uint64 { return a.Cancelled }, known, false},
		{"stopped", "Остановки", "шт", func(a *WorkerAnalysis) uint64 { return a.Stopped }, known, false},
		{"max_running", "Наблюдаемый максимум параллелизма", "шт", func(a *WorkerAnalysis) uint64 { return a.MaxRunning }, known, false},
		{"run_p95_ms", "Длительность попытки p95", "мс", func(a *WorkerAnalysis) uint64 { return a.RunP95MS }, func(a *WorkerAnalysis) bool { return a.RunSamples > 0 }, false},
		{"wait_p95_ms", "Ожидание p95 по захваченному enqueue", "мс", func(a *WorkerAnalysis) uint64 { return a.WaitP95MS }, func(a *WorkerAnalysis) bool { return a.WaitSamples > 0 }, false},
	}
	for _, m := range metrics {
		value := func(s Summary) *float64 {
			if s.WorkerAnalysis == nil || !m.known(s.WorkerAnalysis) || m.rate && s.DurationMS == 0 {
				return nil
			}
			v := float64(m.value(s.WorkerAnalysis))
			if m.rate {
				v = v * 60000 / float64(s.DurationMS)
			}
			return &v
		}
		row := WorkerDiagnosticMetric{Key: m.key, Label: m.label, Unit: m.unit, Baseline: value(before), Candidate: value(after)}
		row.Comparable = compatible && row.Baseline != nil && row.Candidate != nil
		if row.Comparable {
			delta := *row.Candidate - *row.Baseline
			row.Delta = &delta
		}
		result.Lifecycle = append(result.Lifecycle, row)
	}
	result.Catalog = compareWorkerCatalogs(before.WorkerCatalog, after.WorkerCatalog)
	if scope.Comparability != ScenarioFull {
		result.Catalog.Comparable = false
		result.Catalog.Note += " Соответствие приложений и сценариев не подтверждено."
	}
	result.Registry = compareWorkerRegistries(before.WorkRegistry, after.WorkRegistry, workerDiagnosticScopeCoverage(before, after, scope))
	result.Classes, result.OmittedClasses = compareWorkerClasses(before, after, compatible)
	return result
}

const workerComparisonClassLimit = 200

func compareWorkerClasses(before, after Summary, coverage bool) ([]WorkerClassComparison, uint64) {
	// Runtime class names may be obfuscated differently in two APKs. Verified mappings
	// restore source names, even when the two mapping files have different digests.
	stableNames := before.MappingIdentity.Applied && after.MappingIdentity.Applied &&
		before.MappingIdentity.Status == MappingVerified && after.MappingIdentity.Status == MappingVerified
	type counts struct {
		before, after                    uint64
		beforeSeen, afterSeen, ambiguous bool
	}
	byName := make(map[string]counts)
	for _, side := range []struct {
		summary  Summary
		baseline bool
	}{{before, true}, {after, false}} {
		if side.summary.WorkerAnalysis == nil {
			continue
		}
		for _, worker := range side.summary.WorkerAnalysis.Workers {
			if worker.Worker == "" {
				continue
			}
			row := byName[worker.Worker]
			if side.baseline {
				if row.beforeSeen {
					row.ambiguous = true
				}
				row.beforeSeen, row.before = true, worker.Started
			} else {
				if row.afterSeen {
					row.ambiguous = true
				}
				row.afterSeen, row.after = true, worker.Started
			}
			byName[worker.Worker] = row
		}
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	var omitted uint64
	if len(names) > workerComparisonClassLimit {
		omitted = uint64(len(names) - workerComparisonClassLimit)
		names = names[:workerComparisonClassLimit]
	}
	rows := make([]WorkerClassComparison, 0, len(names))
	for _, name := range names {
		count := byName[name]
		row := WorkerClassComparison{Class: name, BaselineStarts: count.before, CandidateStarts: count.after,
			BaselineKnown: before.WorkerAnalysis != nil, CandidateKnown: after.WorkerAnalysis != nil,
			Comparable: coverage && stableNames && !count.ambiguous}
		if row.Comparable {
			delta := float64(count.after) - float64(count.before)
			row.DeltaStarts = &delta
		}
		rows = append(rows, row)
	}
	return rows, omitted
}

func workerComparisonCoverage(a, b Summary, scope ComparisonScope) bool {
	if !workerDiagnosticScopeCoverage(a, b, scope) || a.WorkerAnalysis == nil || b.WorkerAnalysis == nil {
		return false
	}
	for _, w := range []*WorkerAnalysis{a.WorkerAnalysis, b.WorkerAnalysis} {
		if w.DroppedLifecycleEvents > 0 || w.MissingStart > 0 || w.MissingFinish > 0 {
			return false
		}
	}
	return true
}

func workerDiagnosticScopeCoverage(a, b Summary, scope ComparisonScope) bool {
	if !executionObservationCompatible(a, b) {
		return false
	}
	if scope.Comparability != ScenarioFull {
		return false
	}
	for _, s := range []Summary{a, b} {
		q := s.CollectionQuality
		if !q.Complete || !q.ProcessRosterComplete || s.CollectorFlagsAll&uint64(jhlog.CollectorWorker) == 0 {
			return false
		}
	}
	return a.CollectionQuality.ProcessScope != "" && a.CollectionQuality.ProcessScope == b.CollectionQuality.ProcessScope &&
		a.CollectionQuality.ProcessScopeFingerprint != "" && a.CollectionQuality.ProcessScopeFingerprint == b.CollectionQuality.ProcessScopeFingerprint
}
