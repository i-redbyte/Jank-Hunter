package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestInspectRendersAsyncGCAndStartupDetailsWithInterpretationBoundaries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.html")
	summary := analyze.Summary{
		Title: "runtime.jhlog", CollectionQuality: sampleCollectionQuality(),
		AsyncAnalysis: &analyze.AsyncAnalysis{
			Executors: []analyze.AsyncExecutorStats{{
				Name: "render", Started: 10, Failures: 2, WaitSamples: 10, AvgWaitMS: 80,
				MaxWaitMS: 300, ServiceSamples: 10, AvgServiceMS: 150, MaxServiceMS: 700,
				QueueSamples: 4, AvgQueueDepthX100: 250, MaxQueueDepth: 5,
				ActiveSamples: 4, AvgActiveCountX100: 300, MaxActiveCount: 4,
				MaxPoolSize: 4, CompletedHighWatermark: 8,
			}},
			Tasks: []analyze.AsyncTaskStats{{
				Kind: "coroutine", Owner: "FeedRepository", DurationSamples: 2,
				AvgDurationMS: 500, MaxDurationMS: 700, Failures: 1,
			}},
		},
		GCAnalysis: &analyze.GCAnalysis{
			CollectionCount: 4, TotalTimeMS: 120, BlockingCount: 2, BlockingTimeMS: 80,
			BytesAllocated: 64 << 20, BytesFreed: 32 << 20, AllocationRateSamples: 3,
			AvgAllocationRateBytesPerSec: 20 << 20, MaxAllocationRateBytesPerSec: 30 << 20,
			CollectionWindows: 1, JankyUIWindowsNearGC: 1,
		},
		StartupAnalysis: &analyze.StartupAnalysis{
			ColdResumeSamples: 2, AvgColdResumeMS: 1_800, MaxColdResumeMS: 2_400,
			UIVisibleCount: 2, UIHiddenCount: 1,
			Screens: []analyze.StartupScreenStats{{
				Screen: "Checkout", ResumeSamples: 3, AvgResumeMS: 700, MaxResumeMS: 1_200,
			}},
			Transitions: []analyze.NamedValue{{Name: "Feed → Checkout", Value: 3}},
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
		`href="#async-analysis"`, `id="async-analysis"`, "Очереди и выполнение асинхронных задач",
		"render", "FeedRepository", "не равно чистому времени процессора",
		`href="#gc-analysis"`, `id="gc-analysis"`, "Сборка мусора и скорость выделения памяти",
		"Временная связь, а не доказанная причина", "счётчиков среды Android",
		`href="#startup-analysis"`, `id="startup-analysis"`, "Запуск приложения и переходы между экранами",
		"Первое открытие", "Feed → Checkout", "не время до полной отрисовки",
		"Переходы UI в видимое / скрытое состояние", "события жизненного цикла Activity, а не состояние службы",
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("runtime report misses %q", expected)
		}
	}
	for _, forbidden := range []string{
		"Очереди и жизненный цикл async-задач", "не CPU/service time",
		"GC и скорость аллокаций", "Startup и переходы экранов", "Cold-process first resume",
		"не time-to-fully-drawn",
	} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("runtime report contains untranslated term %q", forbidden)
		}
	}
}
