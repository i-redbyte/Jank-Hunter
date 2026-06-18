package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestInspectAndCompareWriteMathReports(t *testing.T) {
	t.Setenv("JH_LANG", "ru")

	dir := t.TempDir()
	samplePath := filepath.Join(dir, "sample.jhlog")
	if err := runSample([]string{"--out", samplePath}); err != nil {
		t.Fatalf("runSample() error = %v", err)
	}

	inspectPath := filepath.Join(dir, "report.html")
	if err := runInspect([]string{samplePath, "--out", inspectPath}); err != nil {
		t.Fatalf("runInspect() error = %v", err)
	}
	assertFileContains(t, inspectPath, "λ Анализ", `href="report-math.html"`, "Разбор утечек памяти")
	assertFileContains(t, filepath.Join(dir, "report-math.html"), "Математический анализ", "Качество данных", "Разбор утечек памяти", "Робастная статистика", "Точки изменения", "Периодические сигналы", "Сетевые циклы", "Граф причинности", "Сводка разделов", "Справка по методам", "Что измеряет")

	diagnosticsPath := filepath.Join(dir, "instrumentation-diagnostics.jsonl")
	writeDiagnosticsFixture(t, diagnosticsPath)
	inspectWithDiagnosticsPath := filepath.Join(dir, "report-with-diagnostics.html")
	if err := runInspect([]string{
		samplePath,
		"--instrumentation-diagnostics", diagnosticsPath,
		"--out", inspectWithDiagnosticsPath,
	}); err != nil {
		t.Fatalf("runInspect(diagnostics) error = %v", err)
	}
	assertFileContains(t, inspectWithDiagnosticsPath, "ASM диагностика", `href="report-with-diagnostics-diagnostics.html"`)
	assertFileContains(t, filepath.Join(dir, "report-with-diagnostics-diagnostics.html"), "ASM диагностика", "okhttp3.bridge.v3", "FeedOwner")

	comparePath := filepath.Join(dir, "compare.html")
	if err := runCompare([]string{"--baseline", samplePath, "--candidate", samplePath, "--out", comparePath}); err != nil {
		t.Fatalf("runCompare() error = %v", err)
	}
	assertFileContains(t, comparePath, "λ Анализ", `href="compare-math.html"`, "Сравнение утечек памяти")
	assertFileContains(t, filepath.Join(dir, "compare-math.html"), "Математический анализ сравнения", "Качество сравнения", "Сравнение утечек памяти", "Робастная статистика", "Точки изменения", "Периодические сигналы", "Сетевые циклы", "Граф причинности", "Сводка разделов", "Справка по методам", "Поля в compare")

	customComparePath := filepath.Join(dir, "another.custom.name.html")
	if err := runCompare([]string{"--baseline", samplePath, "--candidate", samplePath, "--out", customComparePath}); err != nil {
		t.Fatalf("runCompare(custom name) error = %v", err)
	}
	assertFileContains(t, customComparePath, `href="another.custom.name-math.html"`, `href="another.custom.name-influence.html"`)
	assertFileContains(t, filepath.Join(dir, "another.custom.name-math.html"), `href="another.custom.name-influence.html"`)
}

func TestVersionOutputIsHumanReadable(t *testing.T) {
	var buffer bytes.Buffer
	printVersion(&buffer)

	text := buffer.String()
	if !strings.Contains(text, "Jank Hunter CLI 1.0.0") {
		t.Fatalf("version output missing CLI version: %q", text)
	}
	if !strings.Contains(text, ".jhlog format") {
		t.Fatalf("version output missing log format: %q", text)
	}
}

func TestCommandRegistryRoutesVersionAndUnknownCommands(t *testing.T) {
	var buffer bytes.Buffer
	registry := newCommandRegistry(&buffer)

	if err := registry.run([]string{"version"}); err != nil {
		t.Fatalf("registry version error = %v", err)
	}
	if !strings.Contains(buffer.String(), "Jank Hunter CLI 1.0.0") {
		t.Fatalf("version command output = %q", buffer.String())
	}
	if err := registry.run([]string{"missing"}); err == nil {
		t.Fatal("registry accepted unknown command")
	}
}

func TestProblemsExportsCSVAndJSON(t *testing.T) {
	t.Setenv("JH_LANG", "ru")

	dir := t.TempDir()
	samplePath := filepath.Join(dir, "sample.jhlog")
	if err := runSample([]string{"--out", samplePath}); err != nil {
		t.Fatalf("runSample() error = %v", err)
	}

	csvPath := filepath.Join(dir, "problems.csv")
	if err := runProblems([]string{samplePath, "--out", csvPath}); err != nil {
		t.Fatalf("runProblems(csv) error = %v", err)
	}
	assertFileContains(t, csvPath, "class,method,severity,score,categories,problems,screen,flow,step,route,evidence,recommendation", "lifecycle leak")

	jsonPath := filepath.Join(dir, "problems.json")
	if err := runProblems([]string{samplePath, "--format", "json", "--out", jsonPath}); err != nil {
		t.Fatalf("runProblems(json) error = %v", err)
	}
	assertFileContains(t, jsonPath, `"drill_down"`, `"categories"`, `"recommendation"`)

	leaksPath := filepath.Join(dir, "leaks.csv")
	if err := runProblems([]string{samplePath, "--dataset", "leaks", "--out", leaksPath}); err != nil {
		t.Fatalf("runProblems(leaks csv) error = %v", err)
	}
	assertFileContains(t, leaksPath, "class,holder,screen,flow,step,severity,score,count,max_age_ms,estimated_retained_kb,heap_evidence")

	influencePath := filepath.Join(dir, "influence.csv")
	if err := runProblems([]string{samplePath, "--dataset", "influence", "--out", influencePath}); err != nil {
		t.Fatalf("runProblems(influence csv) error = %v", err)
	}
	assertFileContains(t, influencePath, "record_type,from,to,severity,score,status,runtime_confirmed,count")

	mathFindingsPath := filepath.Join(dir, "math-findings.csv")
	if err := runProblems([]string{samplePath, "--dataset", "math-findings", "--out", mathFindingsPath}); err != nil {
		t.Fatalf("runProblems(math findings csv) error = %v", err)
	}
	assertFileContains(t, mathFindingsPath, "section,severity,title,detail,evidence,recommendation")
}

func TestPresentationModeWritesLinkedReports(t *testing.T) {
	t.Setenv("JH_LANG", "ru")

	dir := t.TempDir()
	samplePath := filepath.Join(dir, "sample.jhlog")
	if err := runSample([]string{"--out", samplePath}); err != nil {
		t.Fatalf("runSample() error = %v", err)
	}

	inspectPath := filepath.Join(dir, "presentation-inspect.html")
	if err := runInspect([]string{samplePath, "--presentation", "--out", inspectPath}); err != nil {
		t.Fatalf("runInspect(presentation) error = %v", err)
	}
	assertFileContains(t, inspectPath, "presentation-page")
	assertFileContains(t, filepath.Join(dir, "presentation-inspect-math.html"), "presentation-page")
	if influencePath := filepath.Join(dir, "presentation-inspect-influence.html"); fileExists(influencePath) {
		assertFileContains(t, influencePath, "presentation-page")
	}

	comparePath := filepath.Join(dir, "presentation-compare.html")
	if err := runCompare([]string{"--baseline", samplePath, "--candidate", samplePath, "--presentation", "--out", comparePath}); err != nil {
		t.Fatalf("runCompare(presentation) error = %v", err)
	}
	assertFileContains(t, comparePath, "presentation-page")
	assertFileContains(t, filepath.Join(dir, "presentation-compare-math.html"), "presentation-page")
	if influencePath := filepath.Join(dir, "presentation-compare-influence.html"); fileExists(influencePath) {
		assertFileContains(t, influencePath, "presentation-page")
	}
}

func TestExportStreamsSampleJSONL(t *testing.T) {
	dir := t.TempDir()
	samplePath := filepath.Join(dir, "sample.jhlog")
	if err := runSample([]string{"--out", samplePath}); err != nil {
		t.Fatalf("runSample() error = %v", err)
	}

	exportPath := filepath.Join(dir, "events.jsonl")
	if err := runExport([]string{samplePath, "--out", exportPath}); err != nil {
		t.Fatalf("runExport() error = %v", err)
	}

	log, err := jhlog.ReadFile(samplePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var expected bytes.Buffer
	if err := jhlog.ExportJSONL(log, &expected); err != nil {
		t.Fatalf("ExportJSONL() error = %v", err)
	}
	actual, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", exportPath, err)
	}
	if !bytes.Equal(actual, expected.Bytes()) {
		t.Fatalf("streaming export output changed\nactual:\n%s\nexpected:\n%s", actual, expected.String())
	}
}

func TestAnalysisOptionsBuilderConsumesSharedFlags(t *testing.T) {
	builder, remaining, err := takeAnalysisOptionsBuilder([]string{
		"--route", "feed",
		"--screen=Home",
		"--owner", "FeedRepository",
		"--class", "CheckoutActivity",
		"--owner-map", "owners.json",
		"--class-graph=graph.jsonl",
		"--instrumentation-diagnostics", "diagnostics.jsonl",
		"sample.jhlog",
	})
	if err != nil {
		t.Fatalf("takeAnalysisOptionsBuilder() error = %v", err)
	}
	if got, want := strings.Join(remaining, ","), "sample.jhlog"; got != want {
		t.Fatalf("remaining = %q, want %q", got, want)
	}
	if builder.filter.RouteContains != "feed" ||
		builder.filter.ScreenContains != "Home" ||
		builder.filter.OwnerContains != "FeedRepository" ||
		builder.filter.ClassContains != "CheckoutActivity" {
		t.Fatalf("filter = %+v", builder.filter)
	}
	if builder.ownerMapPath != "owners.json" ||
		builder.classGraphPath != "graph.jsonl" ||
		builder.diagnosticsPath != "diagnostics.jsonl" {
		t.Fatalf(
			"paths = owner map %q class graph %q diagnostics %q",
			builder.ownerMapPath,
			builder.classGraphPath,
			builder.diagnosticsPath,
		)
	}

	heap, remaining, err := takeHeapInputFlags([]string{
		"--heap-dump", "dump.hprof",
		"--heap-evidence=evidence.json",
		"sample.jhlog",
	}, "heap-dump", "heap-evidence")
	if err != nil {
		t.Fatalf("takeHeapInputFlags() error = %v", err)
	}
	if got, want := strings.Join(remaining, ","), "sample.jhlog"; got != want {
		t.Fatalf("heap remaining = %q, want %q", got, want)
	}
	if heap.dumpRaw != "dump.hprof" || heap.evidenceRaw != "evidence.json" {
		t.Fatalf("heap flags = %+v", heap)
	}
}

func assertFileContains(t *testing.T, path string, needles ...string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	text := string(data)
	if strings.Contains(text, "ZgotmplZ") {
		t.Fatalf("%s contains escaped unsafe template output", path)
	}
	for _, needle := range needles {
		if !strings.Contains(text, needle) {
			t.Fatalf("%s does not contain %q", path, needle)
		}
	}
}

func writeDiagnosticsFixture(t *testing.T, path string) {
	t.Helper()
	data := `{"format":1,"class":"com.app.FeedRepository","methods":3,"ignoredMethods":0,"annotatedMethods":1,"skippedMethods":[{"reason":"constructor","count":1}],"hooks":[{"intent":"okhttp.install_event_listener_factory","signature":"okhttp3.builder.build.v3","bridge":"okhttp3.bridge.v3","method":"client()V","count":2}],"decisions":[{"kind":"disabled","module":"handler","family":"handler","reason":"disabled_by_gate","method":"load()V","count":1}],"annotations":[{"owner":"FeedOwner","screen":"Feed","flow":"feed.open","trace":"refresh","count":1}]}`
	if err := os.WriteFile(path, []byte(data+"\n"), 0o644); err != nil {
		t.Fatalf("write diagnostics fixture: %v", err)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
