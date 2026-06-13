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
	assertFileContains(t, inspectPath, "λ Анализ", `href="report-math.html"`)
	assertFileContains(t, filepath.Join(dir, "report-math.html"), "Математический анализ", "Качество данных", "Робастная статистика", "Точки изменения", "Периодические сигналы", "Сетевые циклы", "Справка по методам", "Что измеряет")

	comparePath := filepath.Join(dir, "compare.html")
	if err := runCompare([]string{"--baseline", samplePath, "--candidate", samplePath, "--out", comparePath}); err != nil {
		t.Fatalf("runCompare() error = %v", err)
	}
	assertFileContains(t, comparePath, "λ Анализ", `href="compare-math.html"`)
	assertFileContains(t, filepath.Join(dir, "compare-math.html"), "Математический анализ сравнения", "Качество сравнения", "Робастная статистика", "Точки изменения", "Периодические сигналы", "Сетевые циклы", "Справка по методам", "Поля в compare")
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
