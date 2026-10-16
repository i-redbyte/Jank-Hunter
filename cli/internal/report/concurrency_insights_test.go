package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestConcurrencyReportStartsWithActionableFindingsAndExactEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "threads.html")
	analysis := &analyze.CausalAnalysis{
		Available: true, Complete: true, Events: 2, AppEntities: 1, FindingGroups: 1,
		Coverage: "Тестовый полный охват.",
		Metrics: analyze.CausalMetrics{ThreadGenerations: 2, ThreadCreated: 1, ThreadStarts: 1,
			TaskSubmissions: 3, RunStarts: 4, WithoutObservedEnd: 1,
			KnownCPUNS: 300_000_000, KnownCPUIntervals: 1, UnknownCPURuns: 2,
			KnownReadBytes: 4096, KnownReadOperations: 1, UnknownReadOperations: 1, MaxObservedSpanNS: 500_000_000},
		CreatorGroups: []analyze.CausalCreatorGroup{{Source: "example.app.Home#launch()V", Line: 27,
			Role: "инициатор/постановка", Responsibility: "app_direct", Entities: 2,
			CausalMetrics: analyze.CausalMetrics{ThreadGenerations: 1, ThreadCreated: 1, ThreadStarts: 1,
				TaskSubmissions: 2, RunStarts: 2, MaxObservedSpanNS: 500_000_000},
			CPUInclusiveNS: 300_000_000, CPUInclusiveRuns: 1}},
		Findings: []analyze.CausalFinding{{Rule: "main_thread_blocking_operation", Title: "Измеренная блокирующая операция в главном потоке",
			Consequence: "Главный поток занят 120 мс.", Source: "example.app.Home#read()V", SourceLine: 42,
			Responsibility: "app_direct", Confidence: "высокая для измеренного TID", NextAction: "Перенесите чтение.", Count: 2,
			Anchor: "causal-example", Proof: []string{"точный event 39"}}},
		EntitiesForReview: []analyze.CausalEntityView{{Kind: uint64(jhlog.CausalOperation), ID: 7, Anchor: "causal-example",
			Source: "example.app.Home#read()V", SourceLine: 42, DirectOrigin: "PROJECT", Responsibility: "app_direct",
			FirstNS: 10, ObservedSpanNS: 1_000_000,
			Events: []analyze.CausalEventView{{Phase: 1, ObservedNS: 1_000_010, RelativeNS: 1_000_000, TargetTID: 42,
				KnownFields: jhlog.CausalHasDurationNanos | jhlog.CausalHasReadBytes, DurationNS: 120_000_000, ReadBytes: 0}}}},
		AllEntities: []analyze.CausalEntityView{{Kind: uint64(jhlog.CausalOperation), ID: 7, Anchor: "causal-example",
			Source: "</script><img src=x onerror=alert(1)>", Responsibility: "app_direct"}},
	}
	if err := WriteThreadInspectWithOptions(path, analyze.Summary{CausalAnalysis: analysis}, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(payload)
	for _, want := range []string{"Что мешает работе приложения", "example.app.Home#read()V:42", "Главный поток занят 120 мс", "Перенесите чтение", "Повторений: 2", "href=\"#causal-example\"", "id=\"causal-example\"", "120.000 мс", "0 байт", "CPU: не измерено", "Где наблюдалась работа и сколько она стоила", "example.app.Home#launch()V:27", "неперекрывающиеся интервалы", "байты неизвестны", "+1 мс от первого события"} {
		if !strings.Contains(html, want) {
			t.Errorf("report missing %q", want)
		}
	}
	if strings.Contains(html, "</script><img") || !strings.Contains(html, "\\u003c/script\\u003e") {
		t.Fatal("causal JSON did not escape a script-breaking source")
	}
	if strings.Index(html, "Что мешает работе приложения") > strings.Index(html, "Потоки и корутины</h2>") && strings.Contains(html, "Потоки и корутины</h2>") {
		t.Fatal("actionable findings must precede raw thread section")
	}
	if strings.Index(html, "Главный поток занят 120 мс") > strings.Index(html, "aria-label=\"Измеренная работа приложения\"") {
		t.Fatal("first actionable finding was buried under metric cards")
	}
}

func TestConcurrencyReportShowsReviewFirstWhenNoConfirmedFinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "review.html")
	analysis := &analyze.CausalAnalysis{Available: true, AppEntities: 1,
		Observations: []analyze.CausalFinding{
			{Title: "Проверить очередь A", Source: "app.A#run()V", Responsibility: "app_direct", Anchor: "causal-a"},
			{Title: "Проверить очередь B", Source: "app.B#run()V", Responsibility: "app_direct", Anchor: "causal-b"},
			{Title: "Проверить очередь C", Source: "app.C#run()V", Responsibility: "app_direct", Anchor: "causal-c"},
			{Title: "Проверить очередь D", Source: "app.D#run()V", Responsibility: "app_direct", Anchor: "causal-d"},
		}}
	if err := WriteThreadInspectWithOptions(path, analyze.Summary{CausalAnalysis: analysis}, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(payload)
	metricsAt := strings.Index(html, "aria-label=\"Измеренная работа приложения\"")
	if metricsAt < 0 || strings.Index(html, "Проверить очередь A") > metricsAt || strings.Index(html, "Проверить очередь C") > metricsAt ||
		strings.Index(html, "Проверить очередь D") < metricsAt || strings.Count(html, "Проверить очередь A") != 1 ||
		!strings.Contains(html, "не доказывают дефект") {
		t.Fatal("review observations were duplicated, hidden behind metrics, or mislabeled as confirmed problems")
	}
}

func TestConcurrencyReportDistinguishesCoroutineCancellationFromCompletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coroutine.html")
	entity := analyze.CausalEntityView{Kind: uint64(jhlog.CausalCoroutine), ID: 4,
		Anchor: "causal-coroutine-4", Source: "app.Screen#launch()V", Responsibility: "app_direct",
		LifecycleNote: "вызов отмены наблюдался; без подтверждённого callback терминальный исход и остановка выполнения неизвестны",
		Events:        []analyze.CausalEventView{{Phase: 4, CallResult: 0}, {Phase: 4, CallResult: 1}, {Phase: 4, CallResult: 3}}}
	analysis := &analyze.CausalAnalysis{Available: true, Entities: 1, EntitiesForReview: []analyze.CausalEntityView{entity},
		AllEntities: []analyze.CausalEntityView{entity}}
	if err := WriteThreadInspectWithOptions(path, analyze.Summary{CausalAnalysis: analysis}, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(payload)
	for _, want := range []string{"Запрошена отмена", "Вызов отмены вернулся", "Вызов отмены выбросил исключение",
		"терминальный исход и остановка выполнения неизвестны"} {
		if !strings.Contains(html, want) {
			t.Errorf("coroutine cancellation rendering missing %q", want)
		}
	}
}

func TestConcurrencyReportOldLogHonestFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.html")
	if err := WriteThreadInspectWithOptions(path, analyze.Summary{}, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), "причинные события недоступны") || strings.Contains(string(payload), "Подтверждённых проблем по доступным") {
		t.Fatal("old log was described as complete causal analysis")
	}
}

func TestHandlerRequestedDelayKnownZeroIsNotReportedAsQueueWait(t *testing.T) {
	path := filepath.Join(t.TempDir(), "handler.html")
	entity := analyze.CausalEntityView{Kind: uint64(jhlog.CausalHandlerPost), ID: 4,
		Anchor: "causal-handler-4", Source: "app.Screen#schedule()V", Responsibility: "app_direct",
		Events: []analyze.CausalEventView{{Phase: 1, KnownFields: jhlog.CausalHasRequestedDelay, RequestedDelayNS: 0}}}
	analysis := &analyze.CausalAnalysis{Available: true, Entities: 1, EntitiesForReview: []analyze.CausalEntityView{entity},
		AllEntities: []analyze.CausalEntityView{entity}}
	if err := WriteThreadInspectWithOptions(path, analyze.Summary{CausalAnalysis: analysis}, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(payload)
	if !strings.Contains(html, "Запрошенная задержка Handler: 0.000 мс; это не измеренное ожидание очереди") ||
		!strings.Contains(html, "↓ не измерено · ↑ не измерено") ||
		strings.Contains(html, "Задача ждала в очереди") {
		t.Fatal("known-zero Handler delay was confused with queue wait")
	}
}

func TestResourceUseDoesNotAppearAsAcquisitionInReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resource-use.html")
	entity := analyze.CausalEntityView{Kind: uint64(jhlog.CausalResource), ID: 9,
		Anchor: "causal-resource-9", Source: "app.Store#use()V", Responsibility: "app_direct",
		Events: []analyze.CausalEventView{{Phase: 7, ObservedNS: 10, RelativeNS: 0,
			ResourceKind: jhlog.CausalResourceAutoCloseable},
			{Phase: 4, ObservedNS: 20, RelativeNS: 10,
				ResourceKind: jhlog.CausalResourceAutoCloseable}}}
	analysis := &analyze.CausalAnalysis{Available: true, Entities: 1, EntitiesForReview: []analyze.CausalEntityView{entity},
		AllEntities: []analyze.CausalEntityView{entity},
		Metrics:     analyze.CausalMetrics{ResourcesAcquired: 0, ResourcesObservedUse: 1, ResourcesReleased: 1}}
	if err := WriteThreadInspectWithOptions(path, analyze.Summary{CausalAnalysis: analysis}, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(payload)
	for _, want := range []string{"захват 0 · use переданного ресурса 1 · release 1",
		"Передан в use; приобретение и открытость неизвестны", "не устанавливает, где ресурс приобретён"} {
		if !strings.Contains(html, want) {
			t.Errorf("resource use report missing %q", want)
		}
	}
}
