package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestWriteBundleEmbedsPagesAndNavigationBridge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.html")
	err := WriteBundle(path, []BundlePage{
		{
			ID:    "overview",
			Title: "Обзор",
			Href:  "report.html",
			HTML:  []byte(`<!doctype html><html><body><a href="#network">Сеть</a><a href="report-math.html">Математика</a><section id="network">Сеть</section></body></html>`),
		},
		{
			ID:    "math",
			Title: "Математический анализ",
			Href:  "report-math.html",
			HTML:  []byte(`<!doctype html><html><body><a href="report.html">Обзор</a></body></html>`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, marker := range []string{
		`data-jankhunter-single-html`,
		`data-report-style="modern"`,
		`class="report-logo"`,
		`РАЗДЕЛЫ ОТЧЁТА`,
		`grid-template-columns: 232px minmax(0, 1fr)`,
		`id="jankhunter-report-pages"`,
		`"id":"overview"`,
		`"id":"math"`,
		`"payload":"jankhunter-report-page-payload-0"`,
		`data-jankhunter-report-payload`,
		`JSON.parse(payload.textContent)`,
		`payload.remove()`,
		`jankhunter-report:navigate`,
		`scrollToFragment`,
		`#page=`,
	} {
		if !strings.Contains(html, marker) {
			t.Fatalf("bundle does not contain %q", marker)
		}
	}
	if strings.Count(html, bundledPageBridge) != 0 {
		t.Fatal("embedded bridge must be JSON escaped inside the bundle payload")
	}

	payloadStart := strings.Index(html, `type="application/json">`)
	if payloadStart < 0 {
		t.Fatal("bundle payload script not found")
	}
	payloadStart += len(`type="application/json">`)
	payloadEnd := strings.Index(html[payloadStart:], `</script>`)
	if payloadEnd < 0 {
		t.Fatal("bundle payload closing script not found")
	}
	payloadEnd += payloadStart
	var pages []encodedBundlePage
	if err := json.Unmarshal([]byte(html[payloadStart:payloadEnd]), &pages); err != nil {
		t.Fatalf("decode embedded pages: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("embedded pages = %d, want 2", len(pages))
	}
	if strings.Contains(html[payloadStart:payloadEnd], `"html"`) {
		t.Fatal("page manifest eagerly embeds decoded HTML")
	}
	firstPage := decodeBundlePagePayload(t, html, pages[0].Payload)
	secondPage := decodeBundlePagePayload(t, html, pages[1].Payload)
	if !strings.Contains(firstPage, bundledPageBridge) || !strings.Contains(secondPage, bundledPageBridge) {
		t.Fatal("navigation bridge is not injected into every embedded page")
	}
}

func TestWriteScriptSafeJSONStringKeepsHTMLCompactAndRawTextSafe(t *testing.T) {
	input := `<!doctype html><script>const marker = "</script>";</script><div>данные & контекст</div>`
	var encoded strings.Builder
	if err := writeScriptSafeJSONString(&encoded, input); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encoded.String(), `</script>`) {
		t.Fatal("encoded JSON can terminate its raw-text script container")
	}
	if !strings.Contains(encoded.String(), `<\/script>`) {
		t.Fatal("closing script tag is not slash-escaped")
	}
	if strings.Contains(encoded.String(), `\u003c`) {
		t.Fatal("ordinary HTML tags retain expensive JSON HTML escaping")
	}
	var decoded string
	if err := json.Unmarshal([]byte(encoded.String()), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != input {
		t.Fatalf("decoded payload changed:\n got %q\nwant %q", decoded, input)
	}

	var split strings.Builder
	writer := &scriptSafeJSONWriter{target: &split}
	if _, err := writer.Write([]byte("<")); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("/script>")); err != nil {
		t.Fatal(err)
	}
	if err := writer.flush(); err != nil {
		t.Fatal(err)
	}
	if got := split.String(); got != `<\/script>` {
		t.Fatalf("split raw-text escape = %q, want %q", got, `<\/script>`)
	}
}

func decodeBundlePagePayload(t *testing.T, document, payloadID string) string {
	t.Helper()
	marker := `<script id="` + payloadID + `" type="application/json" data-jankhunter-report-payload>`
	start := strings.Index(document, marker)
	if start < 0 {
		t.Fatalf("bundle payload %q not found", payloadID)
	}
	start += len(marker)
	end := strings.Index(document[start:], `</script>`)
	if end < 0 {
		t.Fatalf("bundle payload %q has no terminator", payloadID)
	}
	var page string
	if err := json.Unmarshal([]byte(document[start:start+end]), &page); err != nil {
		t.Fatalf("decode bundle payload %q: %v", payloadID, err)
	}
	return page
}

func TestWriteBundleWithOptionsPreservesLegacyShell(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.html")
	err := WriteBundleWithOptions(path, []BundlePage{{
		ID:    "overview",
		Title: "Обзор",
		Href:  "report.html",
		HTML:  []byte("<!doctype html><html><body>legacy</body></html>"),
	}}, ReportOptions{Style: ReportStyleLegacy})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, marker := range []string{
		`data-report-style="legacy"`,
		`<div class="report-brand">Jank <span>Hunter</span></div>`,
		`grid-template-rows: auto minmax(0, 1fr)`,
	} {
		if !strings.Contains(html, marker) {
			t.Fatalf("legacy bundle does not contain %q", marker)
		}
	}
	if strings.Contains(html, `class="report-logo"`) {
		t.Fatal("legacy bundle contains modern logo")
	}
}

func TestWriteBundleRejectsDuplicatePageIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.html")
	err := WriteBundle(path, []BundlePage{
		{ID: "overview", Title: "Обзор", Href: "report.html", HTML: []byte("first")},
		{ID: "overview", Title: "Еще обзор", Href: "other.html", HTML: []byte("second")},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate report bundle page id") {
		t.Fatalf("WriteBundle error = %v, want duplicate id error", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("invalid bundle output exists: %v", statErr)
	}
}

func TestWriteLargeBundlePerformanceFixture(t *testing.T) {
	path := os.Getenv("JH_LARGE_BUNDLE_OUT")
	if path == "" {
		t.Skip("set JH_LARGE_BUNDLE_OUT to write the browser performance fixture")
	}

	const pageCount = 6
	const rowCount = 12_000
	pages := make([]BundlePage, 0, pageCount)
	for page := range pageCount {
		pages = append(pages, BundlePage{
			ID:    fmt.Sprintf("page-%d", page),
			Title: fmt.Sprintf("Большой раздел %d", page+1),
			Href:  fmt.Sprintf("large-page-%d.html", page),
			HTML:  largeBundleDocument(page, rowCount),
		})
	}
	if err := WriteBundle(path, pages); err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("large bundle html=%d bytes pages=%d rows/page=%d", stat.Size(), pageCount, rowCount)

	deferredPath := os.Getenv("JH_DEFERRED_REPORT_OUT")
	if deferredPath == "" {
		return
	}
	counters := make([]analyze.NamedValue, rowCount)
	for index := range counters {
		counters[index] = analyze.NamedValue{
			Name:  fmt.Sprintf("performance.counter.%05d", index),
			Value: uint64(index),
		}
	}
	if err := WriteInspectWithOptions(deferredPath, analyze.Summary{
		Title:    "Большая таблица",
		Counters: counters,
	}, ReportOptions{}); err != nil {
		t.Fatal(err)
	}

	registryPath := os.Getenv("JH_DEFERRED_REGISTRY_OUT")
	if registryPath == "" {
		return
	}
	leaks := make([]analyze.LeakReportItem, 300)
	for index := range leaks {
		leaks[index] = analyze.LeakReportItem{
			Rank: index + 1,
			Suspect: analyze.MemoryLeakSuspect{
				ClassName:      fmt.Sprintf("com.performance.DeferredLeak%03d", index),
				Holder:         "com.performance.Owner",
				Score:          float64(300 - index),
				Severity:       "medium",
				ObjectKind:     "object",
				Recommendation: "Проверить время жизни объекта.",
			},
		}
	}
	if err := WriteLeakInspectWithOptions(registryPath, analyze.LeakReport{Items: leaks}, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
}

func largeBundleDocument(page, rowCount int) []byte {
	var document strings.Builder
	document.Grow(rowCount * 320)
	fmt.Fprintf(
		&document,
		`<!doctype html><html lang="ru"><head><meta charset="utf-8"><style>%s%s</style></head><body data-report-style="modern"><main><section class="panel"><h1>Большой раздел %d</h1><table><thead><tr><th>Класс</th><th>Метрика</th><th>Значение</th><th>Детали</th></tr></thead><tbody>`,
		baseCSS,
		modernCSS,
		page+1,
	)
	for row := range rowCount {
		fmt.Fprintf(
			&document,
			`<tr><td><code>com.production.feature%05d.LongClassName%05d.render</code></td><td>executor.queue.wait.p95</td><td>%d</td><td>Длинное диагностическое объяснение строки %d с контекстом экрана, сценария, маршрута и рекомендацией для проверки производительности приложения.</td></tr>`,
			row,
			row,
			row%10_000,
			row,
		)
	}
	document.WriteString(`</tbody></table></section></main><script>`)
	document.WriteString(reportJS)
	document.WriteString(`</script></body></html>`)
	return []byte(document.String())
}
