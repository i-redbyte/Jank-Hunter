package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestAttemptReportKeepsUnknownCompletionAndEscapesWorkerNames(t *testing.T) {
	start := uint64(10)
	summary := analyze.Summary{WorkerAnalysis: &analyze.WorkerAnalysis{Attempts: []analyze.WorkerAttempt{{Worker: "evil</script><img>", StartMS: &start, Coverage: "missing_finish"}}}}
	path := filepath.Join(t.TempDir(), "attempts.html")
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, want := range []string{"data-worker-attempts", "Попытки выполнения", "не доказывает продолжение", "data-attempt-timeline", "data-attempt-thread-start", "data-attempt-thread-end", "data-attempt-thread-link"} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(html, "evil</script><img>") {
		t.Fatal("unsafe attempt content")
	}
}

func TestWorkerReportDisclosesCorrelationLossAndUnknownDurations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worker-loss.html")
	if err := WriteInspectWithOptions(path, analyze.Summary{WorkerAnalysis: &analyze.WorkerAnalysis{DroppedCorrelationEvents: 42}}, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	assertHTMLContains(t, path, "Пропущено событий временной корреляции: 42", "Ожидание запуска неизвестно: нет полной цепочки", "Длительность выполнения неизвестна: нет завершённых попыток")
}
