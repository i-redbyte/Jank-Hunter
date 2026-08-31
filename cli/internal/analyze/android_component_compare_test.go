package analyze

import (
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestCompareAndroidComponentsIncludesLifecycleBinderAndProcessMetrics(t *testing.T) {
	baseline := androidComparisonSummary(false)
	candidate := androidComparisonSummary(false)
	candidate.AndroidComponents.Services.Failures = 8
	candidate.AndroidComponents.Receivers.AsyncDeadlineRisks = 5
	candidate.AndroidComponents.Binder.P95ClientDurationUS = 40_000
	candidate.AndroidComponents.Binder.SlowMainThreadCalls = 12
	candidate.AndroidComponents.ProcessState.HiddenForegroundServiceSamples = 40

	comparison := Compare(baseline, candidate).AndroidComponents
	for _, name := range []string{
		"Service failure rate", "Receiver async deadline risk rate", "Binder client p95",
		"Binder slow main-thread rate", "Binder correlation coverage", "Hidden foreground-service share",
	} {
		metric, ok := androidComparisonMetric(comparison.Metrics, name)
		if !ok || !metric.Comparable {
			t.Fatalf("metric %q = %+v, all = %+v", name, metric, comparison.Metrics)
		}
	}
	if metric, _ := androidComparisonMetric(comparison.Metrics, "Binder client p95"); metric.CandidateValue != 40_000 {
		t.Fatalf("Binder p95 = %+v", metric)
	}
}

func TestCompareAndroidComponentsAllowsPartialMetricsButRejectsCrossProcessCoverage(t *testing.T) {
	baseline := androidComparisonSummary(false)
	candidate := androidComparisonSummary(true)
	comparison := Compare(baseline, candidate).AndroidComponents

	if !strings.Contains(comparison.Note, "частич") {
		t.Fatalf("note = %q", comparison.Note)
	}
	service, _ := androidComparisonMetric(comparison.Metrics, "Service failure rate")
	correlation, _ := androidComparisonMetric(comparison.Metrics, "Binder correlation coverage")
	if !service.Comparable || correlation.Comparable || !strings.Contains(correlation.ComparisonNote, "полный") {
		t.Fatalf("service=%+v correlation=%+v", service, correlation)
	}
}

func androidComparisonSummary(partial bool) Summary {
	return Summary{
		CollectionQuality: CollectionQuality{ProcessScope: jhlog.ProcessScopeAll.String()},
		AndroidComponents: &AndroidComponentAnalysis{
			Available: true, Partial: partial,
			PartialReasons: func() []string {
				if partial {
					return []string{"записано 1 из 2 ожидаемых процессов"}
				}
				return nil
			}(),
			ProcessState: AndroidProcessStateStats{Samples: 100, HiddenForegroundServiceSamples: 20},
			Services:     AndroidServiceAnalysis{Callbacks: 40, Failures: 2, Timeouts: 1, SlowCallbacks: 3},
			Receivers: AndroidReceiverAnalysis{
				Completed: 50, Failures: 2, AsyncCompleted: 20, AsyncDeadlineRisks: 2, SyncSlowCallbacks: 3,
			},
			Binder: AndroidBinderAnalysis{
				ClientCalls: 80, ServerCalls: 75, MainThreadClientCalls: 40, SlowMainThreadCalls: 4,
				Failures: 2, Unhandled: 1, CorrelatedPairs: 70, P95ClientDurationUS: 20_000,
			},
		},
	}
}

func androidComparisonMetric(metrics []Delta, name string) (Delta, bool) {
	for _, metric := range metrics {
		if metric.Name == name {
			return metric, true
		}
	}
	return Delta{}, false
}
