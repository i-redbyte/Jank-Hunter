package report

import (
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestInspectAsyncPagesAreIndependent(t *testing.T) {
	dir := t.TempDir()
	paths := PathsFor(filepath.Join(dir, "report.html"))
	summary := analyze.Summary{
		Title:          "sample.jhlog",
		ThreadAnalysis: &analyze.ThreadAnalysis{Processes: []analyze.ThreadProcessAnalysis{{Process: "com.example.app"}}, CoroutineSegments: []analyze.ThreadCoroutineSegment{{Owner: "com.example.app.Main.load"}}},
		WorkerAnalysis: &analyze.WorkerAnalysis{Instances: 1},
	}
	links := ReportLinks{Main: filepath.Base(paths.Main), Threads: filepath.Base(paths.Threads), Workers: filepath.Base(paths.Workers)}
	if err := WriteInspectWithOptions(paths.Main, summary, ReportOptions{Links: links}); err != nil {
		t.Fatal(err)
	}
	assertHTMLContains(t, paths.Main, `href="report-threads.html"`, `href="report-workers.html"`)
	assertHTMLNotContains(t, paths.Main, `id="threads"`, `id="workers"`)
	if err := WriteThreadInspectWithOptions(paths.Threads, summary, ReportOptions{Links: links}); err != nil {
		t.Fatal(err)
	}
	assertHTMLContains(t, paths.Threads, `id="threads"`, `data-thread-origin`, `data-coroutine-origin`, `data-thread-overview-metrics`, `data-thread-overview-list`, `График и исходные снимки потоков`, `href="report-workers.html"`)
	assertHTMLNotContains(t, paths.Threads, `id="workers"`, `id="overview"`)
	if err := WriteWorkerInspectWithOptions(paths.Workers, summary, ReportOptions{Links: links}); err != nil {
		t.Fatal(err)
	}
	assertHTMLContains(t, paths.Workers, `id="workers"`, `Итоги WorkManager`, `Без наблюдаемого finish`, `href="report-threads.html"`)
	assertHTMLNotContains(t, paths.Workers, `id="threads"`, `id="overview"`)
}
