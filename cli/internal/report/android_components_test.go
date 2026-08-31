package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestInspectRendersInterpretableAndroidComponentsAndIPCSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "android-components.html")
	summary := analyze.Summary{
		Title: "components.jhlog", CollectionQuality: sampleCollectionQuality(),
		AndroidComponents: &analyze.AndroidComponentAnalysis{
			Available: true, Partial: true, PartialReasons: []string{"наблюдается 1 из 2 ожидаемых процессов"},
			Coverage: analyze.AndroidComponentCoverage{
				CatalogAvailable: true, Components: 2, Full: 1, Partial: 1,
				EntryPoints: 4, InstrumentedEntryPoints: 3, UncoveredEntryPoints: 1,
			},
			ProcessState: analyze.AndroidProcessStateStats{Samples: 10, HiddenForegroundServiceSamples: 6},
			Services: analyze.AndroidServiceAnalysis{Callbacks: 3, ForegroundEntries: 1, ActiveForegroundAtEnd: 1,
				Components: []analyze.AndroidServiceStats{{Component: "com.example.SyncService", Process: ":sync", Callbacks: 3}}},
			Receivers: analyze.AndroidReceiverAnalysis{Completed: 2, AsyncCompleted: 1,
				Components: []analyze.AndroidReceiverStats{{Component: "com.example.SyncReceiver", Action: "com.example.SYNC", Process: "main", Completed: 2}}},
			Binder: analyze.AndroidBinderAnalysis{ClientCalls: 2, ServerCalls: 1, CorrelatedPairs: 1,
				Interfaces: []analyze.AndroidBinderInterfaceStats{{Descriptor: "com.example.ISync", Method: "refresh", TransactionCode: 7, ClientCalls: 2, ServerCalls: 1}},
				Flows:      []analyze.AndroidBinderFlow{{Descriptor: "com.example.ISync", Method: "refresh", TransactionCode: 7, ClientProcess: "main", ServerProcess: ":sync", Confidence: "high", ClaimLevel: "correlated", Evidence: "один запуск, дескриптор и код"}}},
		},
	}
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(payload)
	for _, expected := range []string{
		`href="#android-components"`, `id="android-components"`, "Компоненты Android и IPC",
		"Служба переднего плана без активного окна", "com.example.SyncService", "com.example.SyncReceiver",
		"com.example.ISync", "refresh", "вероятная, а не точная связь", "Статическое покрытие",
		"наблюдается 1 из 2 ожидаемых процессов",
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("Android Components report misses %q", expected)
		}
	}
}

func TestCompareRendersAndroidComponentsMetrics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "android-components-compare.html")
	comparison := analyze.Comparison{AndroidComponents: analyze.AndroidComponentComparison{
		Comparable: true, Note: "Сопоставлены одинаковые наборы процессов.",
		Metrics: []analyze.Delta{{
			Name: "Binder client p95", Baseline: "20.00 мс", Candidate: "40.00 мс", Change: "+100.0%",
			Severity: "high", Confidence: "high", Comparable: true, ComparisonNote: "typed client calls",
		}},
	}}
	if err := WriteCompareReportWithOptions(path, comparison, nil, nil, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(payload)
	for _, expected := range []string{`href="#android-components-compare"`, `id="android-components-compare"`, "Сравнение компонентов Android и IPC", "Граница верхних 5% задержек клиента Binder", "Сопоставлены одинаковые наборы процессов."} {
		if !strings.Contains(html, expected) {
			t.Fatalf("Android Components compare report misses %q", expected)
		}
	}
}
