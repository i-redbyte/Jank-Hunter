package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
