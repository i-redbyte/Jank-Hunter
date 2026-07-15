package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/benchfixture"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"github.com/i-redbyte/jank-hunter/cli/internal/mathanalysis"
	"github.com/i-redbyte/jank-hunter/cli/internal/report"
)

const maxRepresentativeReportBundleBytes int64 = 5 * 1024 * 1024

func TestConfigureCLIGarbageCollectorUsesBoundedMemoryDefault(t *testing.T) {
	restoreGCAndEnvironment := preserveGCAndGOGC(t)
	defer restoreGCAndEnvironment()
	if err := os.Unsetenv("GOGC"); err != nil {
		t.Fatal(err)
	}
	debug.SetGCPercent(137)

	restore := configureCLIGarbageCollector()
	if current := currentGCPercent(); current != 35 {
		t.Fatalf("GC percent = %d, want 35", current)
	}
	restore()
	if current := currentGCPercent(); current != 137 {
		t.Fatalf("restored GC percent = %d, want 137", current)
	}
}

func TestConfigureCLIGarbageCollectorRespectsExplicitGOGC(t *testing.T) {
	restoreGCAndEnvironment := preserveGCAndGOGC(t)
	defer restoreGCAndEnvironment()
	if err := os.Setenv("GOGC", "200"); err != nil {
		t.Fatal(err)
	}
	debug.SetGCPercent(149)

	restore := configureCLIGarbageCollector()
	if current := currentGCPercent(); current != 149 {
		t.Fatalf("GC percent = %d, want explicit runtime value 149", current)
	}
	restore()
	if current := currentGCPercent(); current != 149 {
		t.Fatalf("GC percent after no-op restore = %d, want 149", current)
	}
}

func preserveGCAndGOGC(t *testing.T) func() {
	t.Helper()
	previousGC := currentGCPercent()
	previousGOGC, hadGOGC := os.LookupEnv("GOGC")
	return func() {
		debug.SetGCPercent(previousGC)
		if hadGOGC {
			if err := os.Setenv("GOGC", previousGOGC); err != nil {
				t.Errorf("restore GOGC: %v", err)
			}
		} else if err := os.Unsetenv("GOGC"); err != nil {
			t.Errorf("restore absent GOGC: %v", err)
		}
	}
}

func currentGCPercent() int {
	current := debug.SetGCPercent(-1)
	debug.SetGCPercent(current)
	return current
}

func TestRepresentativeReportBundlesStayWithinBudget(t *testing.T) {
	t.Setenv("JH_LANG", "ru")
	directory := t.TempDir()
	profile, err := benchfixture.ProfileByName("representative")
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(directory, "representative.jhlog")
	if _, err := benchfixture.Write(fixture, profile); err != nil {
		t.Fatal(err)
	}
	diagnostics := filepath.Join(directory, "instrumentation-diagnostics.jsonl")
	writeDiagnosticsFixture(t, diagnostics)

	inspectPath := filepath.Join(directory, "inspect.html")
	if err := runInspect([]string{
		fixture,
		"--instrumentation-diagnostics", diagnostics,
		"--out", inspectPath,
	}); err != nil {
		t.Fatalf("runInspect(representative) error = %v", err)
	}
	assertReportBundleWithinBudget(t, inspectPath)
	assertBundlePageContains(
		t,
		inspectPath,
		"overview",
		"Ребра runtime-графа:</strong> показано 256 из 12925",
		"Полный машинный набор доступен в JSON-выводе",
	)
	assertBundlePageNotContains(t, inspectPath, "overview", `data-search=`)

	candidate := copyFileForTest(t, fixture, filepath.Join(directory, "candidate.jhlog"))
	comparePath := filepath.Join(directory, "compare.html")
	if err := runCompare([]string{
		"--baseline", fixture,
		"--candidate", candidate,
		"--instrumentation-diagnostics", diagnostics,
		"--out", comparePath,
	}); err != nil {
		t.Fatalf("runCompare(representative) error = %v", err)
	}
	assertReportBundleWithinBudget(t, comparePath)
	assertBundlePageContains(t, comparePath, "overview", "Реестр проблем кандидата:</strong> показано 64 из 200")
	assertBundlePageNotContains(t, comparePath, "overview", `data-search=`)
}

func assertReportBundleWithinBudget(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat report %s: %v", path, err)
	}
	if info.Size() > maxRepresentativeReportBundleBytes {
		t.Fatalf("report bundle = %d bytes, want <= %d", info.Size(), maxRepresentativeReportBundleBytes)
	}
	assertNoCompanionReports(t, path)
}

func TestInspectAndCompareWriteMathReports(t *testing.T) {
	t.Setenv("JH_LANG", "ru")

	dir := t.TempDir()
	samplePath := filepath.Join(dir, "sample.jhlog")
	if err := runSample([]string{"--out", samplePath}); err != nil {
		t.Fatalf("runSample() error = %v", err)
	}
	candidatePath := copyFileForTest(t, samplePath, filepath.Join(dir, "candidate.jhlog"))

	inspectPath := filepath.Join(dir, "report.html")
	if err := runInspect([]string{samplePath, "--out", inspectPath}); err != nil {
		t.Fatalf("runInspect() error = %v", err)
	}
	assertFileContains(t, inspectPath, `data-jankhunter-single-html`)
	assertBundlePageContains(t, inspectPath, "overview", "λ Анализ", `href="report-math.html"`, "Утечки памяти", `href="report-leaks.html"`, "Удержания и возможные утечки памяти")
	assertBundlePageContains(t, inspectPath, "math", "Математический анализ", "Качество данных", "Разбор утечек памяти", "Робастная статистика", "Точки изменения", "Периодические сигналы", "Сетевые циклы", "Граф причинности", "Сводка разделов", "Справка по методам", "Что измеряет")
	assertBundlePageContains(t, inspectPath, "leaks", "Удержания и возможные утечки памяти", "Проводник утечек", "Сигналы достижимости", "легкий режим", "Контекст обнаружения удержанного объекта")
	assertNoCompanionReports(t, inspectPath)

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
	assertBundlePageContains(t, inspectWithDiagnosticsPath, "overview", "ASM диагностика", `href="report-with-diagnostics-diagnostics.html"`)
	assertBundlePageContains(t, inspectWithDiagnosticsPath, "diagnostics", "ASM диагностика", "okhttp3.bridge.v3", "FeedOwner")
	assertNoCompanionReports(t, inspectWithDiagnosticsPath)

	diCatalogPath := filepath.Join(dir, "di-catalog.jsonl")
	writeDependencyInjectionFixture(t, diCatalogPath)
	inspectWithDIPath := filepath.Join(dir, "report-with-di.html")
	if err := runInspect([]string{
		samplePath,
		"--di-catalog", diCatalogPath,
		"--out", inspectWithDIPath,
	}); err != nil {
		t.Fatalf("runInspect(di catalog) error = %v", err)
	}
	assertBundlePageContains(t, inspectWithDIPath, "overview", "DI-каталог", `href="report-with-di-di.html"`)
	assertBundlePageContains(
		t,
		inspectWithDIPath,
		"dependency-injection",
		"DI-каталог",
		"DI · BUILD TIME",
		"Build-time DI-связь. Это не ссылка удержания, не runtime-вызов и не доказательство утечки. DI-данные не влияют на score, severity или evidence.",
		"com.app.FeedViewModel",
		"com.app.FeedRepository",
	)
	assertNoCompanionReports(t, inspectWithDIPath)

	comparePath := filepath.Join(dir, "compare.html")
	if err := runCompare([]string{"--baseline", samplePath, "--candidate", candidatePath, "--out", comparePath}); err != nil {
		t.Fatalf("runCompare() error = %v", err)
	}
	assertBundlePageContains(t, comparePath, "overview", "λ Анализ", `href="compare-math.html"`, "Утечки памяти", `href="compare-leaks.html"`, "Сравнение сигналов удержания памяти")
	assertBundlePageContains(t, comparePath, "math", "Математический анализ сравнения", "Качество сравнения", "Сравнение сигналов удержания памяти", "Робастная статистика", "Точки изменения", "Периодические сигналы", "Сетевые циклы", "Граф причинности", "Сводка разделов", "Справка по методам", "Поля в compare")
	assertBundlePageContains(t, comparePath, "leaks", "Сравнение сигналов удержания памяти", "Проводник дельт утечек", "количество сигналов удержания не изменилось")
	assertNoCompanionReports(t, comparePath)

	customComparePath := filepath.Join(dir, "another.custom.name.html")
	if err := runCompare([]string{"--baseline", samplePath, "--candidate", candidatePath, "--out", customComparePath}); err != nil {
		t.Fatalf("runCompare(custom name) error = %v", err)
	}
	assertBundlePageContains(t, customComparePath, "overview", `href="another.custom.name-math.html"`, `href="another.custom.name-leaks.html"`, `href="another.custom.name-influence.html"`)
	assertBundlePageContains(t, customComparePath, "math", `href="another.custom.name-influence.html"`)
	assertNoCompanionReports(t, customComparePath)
}

func TestFailedOptionalCompanionWritesPrimaryOnceWithoutBrokenLink(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "sample.jhlog")
	if err := jhlog.WriteSample(logPath); err != nil {
		t.Fatal(err)
	}
	summary, err := analyze.InspectFilesWithOptions("sample", []string{logPath}, analyze.Options{})
	if err != nil {
		t.Fatal(err)
	}
	primaryPath := filepath.Join(dir, "report.html")
	reportPaths := report.PathsFor(primaryPath)
	if err := os.WriteFile(reportPaths.Math, []byte("stale companion"), 0o600); err != nil {
		t.Fatal(err)
	}

	primaryWrites := 0
	mathWrites := 0
	err = writeInspectReportSetUsing(
		primaryPath,
		summary,
		[]string{logPath},
		analyze.Options{},
		report.ReportOptions{},
		inspectReportSetWriters{
			primary: func(path string, value analyze.Summary, options report.ReportOptions) error {
				primaryWrites++
				return report.WriteInspectWithOptions(path, value, options)
			},
			math: func(string, mathanalysis.MathReport, report.ReportOptions) error {
				mathWrites++
				return errors.New("simulated companion failure")
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if primaryWrites != 1 {
		t.Fatalf("primary writes = %d, want 1", primaryWrites)
	}
	if mathWrites != 1 {
		t.Fatalf("math writes = %d, want 1", mathWrites)
	}
	assertFileNotContains(t, primaryPath, `href="report-math.html"`)
	assertFileContains(t, primaryPath, "математический отчет inspect не записан")
	assertFileContains(t, reportPaths.Math, "stale companion")
}

func TestBuildLogReportsReusesCombinedSingleLogSummary(t *testing.T) {
	combined := analyze.Summary{Title: "combined", LogCount: 1, EventCount: 123}
	reports, err := buildLogReports("baseline", []string{"not-read-again.jhlog"}, analyze.Options{}, combined)
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 || reports[0].Summary.EventCount != combined.EventCount {
		t.Fatalf("reports = %+v", reports)
	}
}

func TestVersionOutputIsHumanReadable(t *testing.T) {
	var buffer bytes.Buffer
	printVersion(&buffer)

	text := buffer.String()
	if !strings.Contains(text, "Jank Hunter CLI 1.0.2") {
		t.Fatalf("version output missing CLI version: %q", text)
	}
	if !strings.Contains(text, ".jhlog format") {
		t.Fatalf("version output missing log format: %q", text)
	}
}

func TestSelectLatestSessionLogsKeepsLatestSessionPerProcess(t *testing.T) {
	dir := t.TempDir()
	newMainEarlierSegment := filepath.Join(dir, "jh-session-log.2026-07-13.8.jhlog")
	oldMain := filepath.Join(dir, "jh-session-log.2026-07-13.9.jhlog")
	newMainLatestSegment := filepath.Join(dir, "jh-session-log.2026-07-13.10.jhlog")
	oldRemote := filepath.Join(dir, "jh-session-log.2026-07-12.500.jhlog")
	newRemote := filepath.Join(dir, "jh-session-log.2026-07-14.1.jhlog")
	nonCanonical := filepath.Join(dir, "sample.jhlog")

	writeSessionSelectionLog(t, newMainEarlierSegment, "com.example", 2, 8)
	writeSessionSelectionLog(t, oldMain, "com.example", 1, 9)
	writeSessionSelectionLog(t, newMainLatestSegment, "com.example", 2, 10)
	writeSessionSelectionLog(t, oldRemote, "com.example:remote", 3, 500)
	writeSessionSelectionLog(t, newRemote, "com.example:remote", 4, 1)

	paths := []string{oldMain, newMainEarlierSegment, newMainLatestSegment, oldRemote, newRemote, nonCanonical}
	selected, warnings := selectLatestSessionLogs(paths, false)

	expected := []string{newMainEarlierSegment, newMainLatestSegment, newRemote, nonCanonical}
	if !sameStrings(selected, expected) {
		t.Fatalf("selected = %#v, want %#v", selected, expected)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %#v, want one warning", warnings)
	}
	for _, skipped := range []string{oldMain, oldRemote} {
		if !strings.Contains(warnings[0], skipped) {
			t.Fatalf("warning %q does not mention skipped file %q", warnings[0], skipped)
		}
	}

	all, warnings := selectLatestSessionLogs(paths, true)
	if !sameStrings(all, paths) {
		t.Fatalf("all sessions = %#v, want %#v", all, paths)
	}
	if len(warnings) != 0 {
		t.Fatalf("all sessions warnings = %#v, want none", warnings)
	}
}

func TestSelectLatestSessionLogsKeepsExplicitNonCanonicalFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.jhlog")
	selected, warnings := selectLatestSessionLogs([]string{path}, false)
	if !sameStrings(selected, []string{path}) || len(warnings) != 0 {
		t.Fatalf("selected=%#v warnings=%#v", selected, warnings)
	}
}

func TestCanonicalizeLogInputsReturnsAbsolutePhysicalFiles(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "sample.jhlog")
	if err := os.WriteFile(path, []byte("sample"), 0o600); err != nil {
		t.Fatal(err)
	}

	resolved, err := canonicalizeLogInputs([]string{filepath.Join(nested, "..", "sample.jhlog")})
	if err != nil {
		t.Fatal(err)
	}
	canonicalPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 || resolved[0] != canonicalPath || !filepath.IsAbs(resolved[0]) {
		t.Fatalf("resolved paths = %#v, want absolute %q", resolved, canonicalPath)
	}
}

func TestCanonicalizeLogInputsDeduplicatesSymlinksAndHardlinks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.jhlog")
	if err := os.WriteFile(path, []byte("sample"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(dir, "sample-symlink.jhlog")
	if err := os.Symlink(path, symlink); err != nil {
		t.Fatal(err)
	}
	hardlink := filepath.Join(dir, "sample-hardlink.jhlog")
	if err := os.Link(path, hardlink); err != nil {
		t.Fatal(err)
	}

	resolved, err := canonicalizeLogInputs([]string{symlink, hardlink, path})
	if err != nil {
		t.Fatal(err)
	}
	canonicalPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	if !sameStrings(resolved, []string{canonicalPath}) {
		t.Fatalf("deduplicated paths = %#v, want %#v", resolved, []string{canonicalPath})
	}
}

func TestCanonicalizeFileInputsDeduplicatesHeapEvidenceByPhysicalIdentity(t *testing.T) {
	dir := t.TempDir()
	heapDump := filepath.Join(dir, "heap.hprof")
	if err := os.WriteFile(heapDump, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	hardlink := filepath.Join(dir, "heap-copy.hprof")
	if err := os.Link(heapDump, hardlink); err != nil {
		t.Fatal(err)
	}

	resolved, err := canonicalizeFileInputs([]string{hardlink, heapDump}, "heap dump")
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 || !filepath.IsAbs(resolved[0]) {
		t.Fatalf("resolved heap inputs = %#v, want one absolute physical file", resolved)
	}
}

func TestRejectLogInputOverlapUsesPhysicalIdentity(t *testing.T) {
	dir := t.TempDir()
	baseline := filepath.Join(dir, "baseline.jhlog")
	if err := os.WriteFile(baseline, []byte("same bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	hardlink := filepath.Join(dir, "hardlink.jhlog")
	if err := os.Link(baseline, hardlink); err != nil {
		t.Fatal(err)
	}
	baselinePaths, err := canonicalizeLogInputs([]string{baseline})
	if err != nil {
		t.Fatal(err)
	}
	hardlinkPaths, err := canonicalizeLogInputs([]string{hardlink})
	if err != nil {
		t.Fatal(err)
	}
	if err := rejectLogInputOverlap("baseline", baselinePaths, "candidate", hardlinkPaths); err == nil || !strings.Contains(err.Error(), "refer to the same file") {
		t.Fatalf("hardlink overlap error = %v", err)
	}

	copyPath := copyFileForTest(t, baseline, filepath.Join(dir, "copy.jhlog"))
	copyPaths, err := canonicalizeLogInputs([]string{copyPath})
	if err != nil {
		t.Fatal(err)
	}
	if err := rejectLogInputOverlap("baseline", baselinePaths, "candidate", copyPaths); err != nil {
		t.Fatalf("independent copy rejected: %v", err)
	}
}

func writeSessionSelectionLog(t *testing.T, path, processName string, sessionByte byte, segmentIndex uint64) {
	t.Helper()
	header := jhlog.DefaultSegmentHeader()
	header.ProcessName = processName
	header.SessionID[0] = sessionByte
	header.ProcessInstanceID[0] = sessionByte
	header.RunID[0] = sessionByte
	header.SegmentIndex = segmentIndex
	file, _, err := jhlog.CreateWithHeader(path, header)
	if err != nil {
		t.Fatalf("CreateWithHeader(%q) error = %v", path, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close(%q) error = %v", path, err)
	}
}

func TestDiscoverHeapDumpsNearLogs(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "jh-session-log.2026-07-14.0.jhlog")
	if err := os.WriteFile(logPath, []byte("jhlog"), 0o600); err != nil {
		t.Fatalf("WriteFile(log) error = %v", err)
	}
	rootHeap := filepath.Join(dir, "retained-1.hprof")
	if err := os.WriteFile(rootHeap, []byte("hprof"), 0o600); err != nil {
		t.Fatalf("WriteFile(root heap) error = %v", err)
	}
	heapDumpDir := filepath.Join(dir, "heap-dumps")
	if err := os.Mkdir(heapDumpDir, 0o700); err != nil {
		t.Fatalf("Mkdir(heap-dumps) error = %v", err)
	}
	legacyHeap := filepath.Join(heapDumpDir, "retained-legacy.hprof")
	if err := os.WriteFile(legacyHeap, []byte("hprof"), 0o600); err != nil {
		t.Fatalf("WriteFile(legacy heap) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("skip"), 0o600); err != nil {
		t.Fatalf("WriteFile(notes) error = %v", err)
	}

	discovered := discoverHeapDumpsNearLogs([]string{logPath, logPath})
	expected := []string{rootHeap, legacyHeap}
	if !sameStrings(discovered, expected) {
		t.Fatalf("heap dumps = %#v, want %#v", discovered, expected)
	}
}

func TestCommandRegistryRoutesVersionAndUnknownCommands(t *testing.T) {
	var buffer bytes.Buffer
	registry := newCommandRegistry(&buffer)

	if err := registry.run([]string{"version"}); err != nil {
		t.Fatalf("registry version error = %v", err)
	}
	if !strings.Contains(buffer.String(), "Jank Hunter CLI 1.0.2") {
		t.Fatalf("version command output = %q", buffer.String())
	}
	if err := registry.run([]string{"missing"}); err == nil {
		t.Fatal("registry accepted unknown command")
	}
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func copyFileForTest(t *testing.T, source, target string) string {
	t.Helper()
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", source, err)
	}
	if err := os.WriteFile(target, raw, 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", target, err)
	}
	return target
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
	assertFileContains(t, csvPath, "class,method,severity,score,categories,problems,screen,flow,step,route,evidence,recommendation", "Утечка жизненного цикла")

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

func TestScorecardWritesValidationJSON(t *testing.T) {
	t.Setenv("JH_LANG", "ru")

	dir := t.TempDir()
	samplePath := filepath.Join(dir, "sample.jhlog")
	if err := runSample([]string{"--out", samplePath}); err != nil {
		t.Fatalf("runSample() error = %v", err)
	}
	candidatePath := copyFileForTest(t, samplePath, filepath.Join(dir, "candidate.jhlog"))

	scorecardPath := filepath.Join(dir, "scorecard.json")
	if err := runScorecard([]string{
		"--baseline", samplePath,
		"--candidate", candidatePath,
		"--out", scorecardPath,
	}); err != nil {
		t.Fatalf("runScorecard() error = %v", err)
	}

	assertFileContains(
		t,
		scorecardPath,
		`"purpose": "Real-world validation readiness scorecard for Jank Hunter Android logs."`,
		`"data_quality"`,
		`"heap_actionability"`,
		`"weighted_score_0_to_10"`,
		`"go_no_go"`,
	)
}

func TestPresentationModeWritesLinkedReports(t *testing.T) {
	t.Setenv("JH_LANG", "ru")

	dir := t.TempDir()
	samplePath := filepath.Join(dir, "sample.jhlog")
	if err := runSample([]string{"--out", samplePath}); err != nil {
		t.Fatalf("runSample() error = %v", err)
	}
	candidatePath := copyFileForTest(t, samplePath, filepath.Join(dir, "candidate.jhlog"))

	inspectPath := filepath.Join(dir, "presentation-inspect.html")
	if err := runInspect([]string{samplePath, "--presentation", "--out", inspectPath}); err != nil {
		t.Fatalf("runInspect(presentation) error = %v", err)
	}
	assertBundlePageContains(t, inspectPath, "overview", "presentation-page")
	assertBundlePageContains(t, inspectPath, "math", "presentation-page")
	assertBundlePageContains(t, inspectPath, "influence", "presentation-page")
	assertNoCompanionReports(t, inspectPath)

	comparePath := filepath.Join(dir, "presentation-compare.html")
	if err := runCompare([]string{"--baseline", samplePath, "--candidate", candidatePath, "--presentation", "--out", comparePath}); err != nil {
		t.Fatalf("runCompare(presentation) error = %v", err)
	}
	assertBundlePageContains(t, comparePath, "overview", "presentation-page")
	assertBundlePageContains(t, comparePath, "math", "presentation-page")
	assertBundlePageContains(t, comparePath, "influence", "presentation-page")
	assertNoCompanionReports(t, comparePath)
}

func TestAnimatedBackgroundFlagDefaultsOff(t *testing.T) {
	dir := t.TempDir()
	samplePath := filepath.Join(dir, "sample.jhlog")
	if err := runSample([]string{"--out", samplePath}); err != nil {
		t.Fatalf("runSample() error = %v", err)
	}
	candidatePath := copyFileForTest(t, samplePath, filepath.Join(dir, "candidate.jhlog"))

	plainPath := filepath.Join(dir, "plain.html")
	if err := runInspect([]string{samplePath, "--out", plainPath}); err != nil {
		t.Fatalf("runInspect(plain) error = %v", err)
	}
	assertBundlePageNotContains(t, plainPath, "overview", `class="animated-background"`)
	assertBundlePageNotContains(t, plainPath, "leaks", `class="leak-page animated-background"`)

	animatedPath := filepath.Join(dir, "animated.html")
	if err := runInspect([]string{samplePath, "--animated-background", "--out", animatedPath}); err != nil {
		t.Fatalf("runInspect(animated) error = %v", err)
	}
	assertBundlePageContains(t, animatedPath, "overview", `class="animated-background"`)
	assertBundlePageContains(t, animatedPath, "leaks", `class="leak-page animated-background"`)
	assertNoCompanionReports(t, animatedPath)

	comparePath := filepath.Join(dir, "animated-compare.html")
	if err := runCompare([]string{"--baseline", samplePath, "--candidate", candidatePath, "--animated-background", "--out", comparePath}); err != nil {
		t.Fatalf("runCompare(animated) error = %v", err)
	}
	assertBundlePageContains(t, comparePath, "overview", `class="animated-background"`)
	assertBundlePageContains(t, comparePath, "leaks", `class="leak-page animated-background"`)
	assertNoCompanionReports(t, comparePath)
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

	var expected bytes.Buffer
	encoder := json.NewEncoder(&expected)
	canonicalPaths, err := resolveLogArgs([]string{samplePath})
	if err != nil {
		t.Fatalf("resolveLogArgs() error = %v", err)
	}
	warnings, err := jhlog.StreamFileWithWarnings(canonicalPaths[0], func(event jhlog.Event, _ map[uint64]string) error {
		return encoder.Encode(event)
	})
	if err != nil {
		t.Fatalf("StreamFileWithWarnings() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected export fixture warnings: %+v", warnings)
	}
	actual, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", exportPath, err)
	}
	if !bytes.Equal(actual, expected.Bytes()) {
		t.Fatalf("streaming export output changed\nactual:\n%s\nexpected:\n%s", actual, expected.String())
	}
}

func TestExportKeepsExistingOutputWhenAStreamFails(t *testing.T) {
	dir := t.TempDir()
	samplePath := filepath.Join(dir, "sample.jhlog")
	if err := runSample([]string{"--out", samplePath}); err != nil {
		t.Fatalf("runSample() error = %v", err)
	}
	brokenPath := filepath.Join(dir, "broken.jhlog")
	if err := os.WriteFile(brokenPath, []byte("not a jhlog"), 0o600); err != nil {
		t.Fatal(err)
	}
	exportPath := filepath.Join(dir, "events.jsonl")
	const previous = "previous complete export\n"
	if err := os.WriteFile(exportPath, []byte(previous), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := runExport([]string{samplePath, brokenPath, "--out", exportPath}); err == nil {
		t.Fatal("runExport() error = nil, want invalid second stream failure")
	}
	actual, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != previous {
		t.Fatalf("failed export replaced complete output: %q", actual)
	}
}

func TestSizeProfilesSampleLog(t *testing.T) {
	dir := t.TempDir()
	samplePath := filepath.Join(dir, "sample.jhlog")
	if err := runSample([]string{"--out", samplePath}); err != nil {
		t.Fatalf("runSample() error = %v", err)
	}

	if err := runSize([]string{samplePath}); err != nil {
		t.Fatalf("runSize() error = %v", err)
	}
	if err := runSize([]string{samplePath, "--json"}); err != nil {
		t.Fatalf("runSize(json) error = %v", err)
	}
}

func TestAnalysisOptionsBuilderConsumesSharedFlags(t *testing.T) {
	builder, remaining, err := takeAnalysisOptionsBuilder([]string{
		"--route", "feed",
		"--screen=Home",
		"--owner", "FeedRepository",
		"--class", "CheckoutActivity",
		"--external-symbols",
		"--artifacts-dir", "app/build/generated/jankhunter/debug",
		"--owner-map", "app-owners.json",
		"--owner-map=feature-owners.json",
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
	if !builder.externalSymbols {
		t.Fatal("--external-symbols was not retained by the shared analysis options builder")
	}
	if strings.Join(builder.ownerMapPaths, ",") != "app-owners.json,feature-owners.json" ||
		builder.artifactsDir != "app/build/generated/jankhunter/debug" ||
		builder.classGraphPath != "graph.jsonl" ||
		builder.diagnosticsPath != "diagnostics.jsonl" {
		t.Fatalf(
			"paths = owner maps %q class graph %q diagnostics %q",
			builder.ownerMapPaths,
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

func TestExternalSymbolsRequireArtifactsOrOwnerMap(t *testing.T) {
	builder, remaining, err := takeAnalysisOptionsBuilder([]string{"--external-symbols", "sample.jhlog"})
	if err != nil {
		t.Fatalf("takeAnalysisOptionsBuilder() error = %v", err)
	}
	if got := strings.Join(remaining, ","); got != "sample.jhlog" {
		t.Fatalf("remaining = %q", got)
	}
	if _, err := builder.build(); err == nil || !strings.Contains(err.Error(), "--artifacts-dir") {
		t.Fatalf("build() error = %v, want explicit external artifact guidance", err)
	}
}

func TestAnalysisOptionsBuilderLoadsCanonicalArtifactBundle(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "app", "build", "generated", "jankhunter", "debug")
	writeAndroidArtifactBundle(t, directory, true)

	builder, remaining, err := takeAnalysisOptionsBuilder([]string{
		"--artifacts-dir", directory,
		"sample.jhlog",
	})
	if err != nil {
		t.Fatalf("takeAnalysisOptionsBuilder() error = %v", err)
	}
	if got := strings.Join(remaining, ","); got != "sample.jhlog" {
		t.Fatalf("remaining = %q", got)
	}
	options, err := builder.build()
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	if options.OwnerMap == nil || len(options.OwnerMap.Entries) != 1 {
		t.Fatalf("owner map = %+v", options.OwnerMap)
	}
	if options.ClassGraph == nil || len(options.ClassGraph.Edges) != 1 {
		t.Fatalf("class graph = %+v", options.ClassGraph)
	}
	if options.InstrumentationDiagnostics == nil || !options.InstrumentationDiagnostics.Available {
		t.Fatalf("diagnostics = %+v", options.InstrumentationDiagnostics)
	}
	if options.DependencyInjectionCatalog == nil || !options.DependencyInjectionCatalog.Available {
		t.Fatalf("DI catalog = %+v", options.DependencyInjectionCatalog)
	}
}

func TestArtifactDiscoverySelectsNewestCoherentVariant(t *testing.T) {
	root := t.TempDir()
	oldDirectory := filepath.Join(root, "app", "build", "generated", "jankhunter", "release")
	newDirectory := filepath.Join(root, "app", "build", "generated", "jankhunter", "debug")
	writeAndroidArtifactBundle(t, oldDirectory, false)
	writeAndroidArtifactBundle(t, newDirectory, false)
	oldTime := time.Now().Add(-time.Hour)
	newTime := time.Now()
	for _, directory := range []string{oldDirectory, newDirectory} {
		stamp := oldTime
		if directory == newDirectory {
			stamp = newTime
		}
		for _, name := range []string{"owner-map.json", "class-graph.jsonl", "instrumentation-diagnostics.jsonl"} {
			if err := os.Chtimes(filepath.Join(directory, name), stamp, stamp); err != nil {
				t.Fatal(err)
			}
		}
	}

	if got := discoverAndroidArtifactDirectory([]string{root}); got != newDirectory {
		t.Fatalf("discovered directory = %q, want %q", got, newDirectory)
	}
}

func TestAnalysisOptionsBuilderRejectsIncompleteArtifactBundle(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "owner-map.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	builder := analysisOptionsBuilder{artifactsDir: directory}
	if _, err := builder.build(); err == nil || !strings.Contains(err.Error(), "class-graph.jsonl") {
		t.Fatalf("build() error = %v, want missing class graph", err)
	}
}

func TestAnalysisOptionsBuilderMergesRepeatedOwnerMaps(t *testing.T) {
	dir := t.TempDir()
	appPath := filepath.Join(dir, "app-owner-map.json")
	featurePath := filepath.Join(dir, "feature-owner-map.json")
	const metadata = `{"format":4,"kind":"metadata","symbolNamespace":"aabb0000000000000000000000000000"}` + "\n"
	if err := os.WriteFile(appPath, []byte(metadata+`{"format":4,"kind":"entry","id":"stable:0x0000000000000001","owner":"com.app.Main.call"}`+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(app) error = %v", err)
	}
	if err := os.WriteFile(featurePath, []byte(metadata+`{"format":4,"kind":"entry","id":"stable:0x0000000000000002","owner":"com.app.feature.Feature.call"}`+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(feature) error = %v", err)
	}

	builder, remaining, err := takeAnalysisOptionsBuilder([]string{
		"--owner-map", appPath,
		"--owner-map=" + featurePath,
		"sample.jhlog",
	})
	if err != nil {
		t.Fatalf("takeAnalysisOptionsBuilder() error = %v", err)
	}
	if got := strings.Join(remaining, ","); got != "sample.jhlog" {
		t.Fatalf("remaining = %q", got)
	}
	options, err := builder.build()
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	if options.OwnerMap == nil || len(options.OwnerMap.Entries) != 2 {
		t.Fatalf("merged owner map = %+v", options.OwnerMap)
	}
	if got := options.OwnerMap.Entries["stable:0x0000000000000002"]; got != "com.app.feature.Feature.call" {
		t.Fatalf("feature owner = %q", got)
	}
}

func TestAnalysisOptionsBuilderRejectsEmptyOwnerMapFlag(t *testing.T) {
	for _, args := range [][]string{{"--owner-map"}, {"--owner-map="}, {"--owner-map", ""}} {
		if _, _, err := takeAnalysisOptionsBuilder(args); err == nil || !strings.Contains(err.Error(), "--owner-map") {
			t.Fatalf("takeAnalysisOptionsBuilder(%q) error = %v, want owner-map value error", args, err)
		}
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

func assertFileNotContains(t *testing.T, path string, needles ...string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	text := string(data)
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			t.Fatalf("%s contains %q", path, needle)
		}
	}
}

type testBundlePage struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Href  string `json:"href"`
	HTML  string `json:"html"`
}

func readBundlePages(t *testing.T, path string) map[string]testBundlePage {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	text := string(data)
	const marker = `<script id="jankhunter-report-pages" type="application/json">`
	start := strings.Index(text, marker)
	if start < 0 {
		t.Fatalf("%s is not a single HTML report", path)
	}
	start += len(marker)
	end := strings.Index(text[start:], `</script>`)
	if end < 0 {
		t.Fatalf("%s has no embedded report payload terminator", path)
	}
	var pages []testBundlePage
	if err := json.Unmarshal([]byte(text[start:start+end]), &pages); err != nil {
		t.Fatalf("decode embedded pages from %s: %v", path, err)
	}
	byID := make(map[string]testBundlePage, len(pages))
	for _, page := range pages {
		byID[page.ID] = page
	}
	return byID
}

func assertBundlePageContains(t *testing.T, path, pageID string, needles ...string) {
	t.Helper()
	page, exists := readBundlePages(t, path)[pageID]
	if !exists {
		t.Fatalf("%s does not contain embedded page %q", path, pageID)
	}
	if strings.Contains(page.HTML, "ZgotmplZ") {
		t.Fatalf("embedded page %q in %s contains escaped unsafe template output", pageID, path)
	}
	for _, needle := range needles {
		if !strings.Contains(page.HTML, needle) {
			t.Fatalf("embedded page %q in %s does not contain %q", pageID, path, needle)
		}
	}
}

func assertBundlePageNotContains(t *testing.T, path, pageID string, needles ...string) {
	t.Helper()
	page, exists := readBundlePages(t, path)[pageID]
	if !exists {
		t.Fatalf("%s does not contain embedded page %q", path, pageID)
	}
	for _, needle := range needles {
		if strings.Contains(page.HTML, needle) {
			t.Fatalf("embedded page %q in %s contains %q", pageID, path, needle)
		}
	}
}

func assertNoCompanionReports(t *testing.T, mainPath string) {
	t.Helper()
	paths := report.PathsFor(mainPath)
	for _, path := range []string{paths.Math, paths.Leaks, paths.Influence, paths.Diagnostics, paths.DependencyInjection} {
		if _, err := os.Stat(path); err == nil {
			t.Fatalf("standalone report unexpectedly created companion %s", path)
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat companion %s: %v", path, err)
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

func writeDependencyInjectionFixture(t *testing.T, path string) {
	t.Helper()
	data := strings.Join([]string{
		`{"format":1,"kind":"metadata","variant":"debug","semantics":"build_time_di","edgeDirection":"consumer_to_dependency","runtimeTracing":false,"affectsScore":false}`,
		`{"format":1,"kind":"class","name":"com.app.FeedViewModel","framework":"hilt","roles":["consumer"],"generated":false,"scopes":[],"components":["dagger.hilt.components.SingletonComponent"]}`,
		`{"format":1,"kind":"edge","consumer":"com.app.FeedViewModel","dependency":"com.app.FeedRepository","framework":"hilt","injectionKind":"constructor","site":"com.app.FeedViewModel#<init>(Lcom/app/FeedRepository;)V","qualifiers":[],"resolution":"declared"}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(data+"\n"), 0o644); err != nil {
		t.Fatalf("write DI catalog fixture: %v", err)
	}
}

func writeAndroidArtifactBundle(t *testing.T, directory string, includeDI bool) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	ownerMap := strings.Join([]string{
		`{"format":4,"kind":"metadata","symbolNamespace":"aabb0000000000000000000000000000"}`,
		`{"format":4,"kind":"entry","id":"stable:0x0000000000000001","owner":"com.app.Feed.call"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(directory, "owner-map.json"), []byte(ownerMap), 0o600); err != nil {
		t.Fatal(err)
	}
	classGraph := `{"format":1,"class":"com.app.Feed","edges":[{"caller":"load()V","calleeClass":"com.app.Repository","calleeMethod":"get()V","count":1}]}` + "\n"
	if err := os.WriteFile(filepath.Join(directory, "class-graph.jsonl"), []byte(classGraph), 0o600); err != nil {
		t.Fatal(err)
	}
	writeDiagnosticsFixture(t, filepath.Join(directory, "instrumentation-diagnostics.jsonl"))
	if includeDI {
		writeDependencyInjectionFixture(t, filepath.Join(directory, "di-catalog.jsonl"))
	}
}
