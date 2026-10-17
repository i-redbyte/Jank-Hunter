package main

import (
	"html/template"
	"os"
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestReportNavigationFixture(t *testing.T) {
	dir := os.Getenv("JH_NAV_OUT")
	if dir == "" {
		dir = t.TempDir()
	}
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{
		filepath.Join(dir, "sample.jhlog"),
		filepath.Join(dir, "release, candidate.jhlog"),
		filepath.Join(dir, "source <file> & data.jhlog"),
	}
	if err := jhlog.WriteSample(paths[0]); err != nil {
		t.Fatal(err)
	}
	for index, path := range paths[1:] {
		writeConfigurationEpochLog(t, path, byte(index+2))
	}
	inspect := filepath.Join(dir, "inspect.html")
	args := append(append([]string{}, paths...), "--all-sessions", "--out", inspect)
	if err := runInspect(args); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		assertBundlePageContains(t, inspect, "overview", `<li class="report-source">`+template.HTMLEscapeString(path)+`</li>`)
	}
	candidate := filepath.Join(dir, "candidate.jhlog")
	if err := jhlog.WriteSample(candidate); err != nil {
		t.Fatal(err)
	}
	if err := runCompare([]string{"--baseline", paths[0], "--candidate", candidate, "--out", filepath.Join(dir, "compare.html")}); err != nil {
		t.Fatal(err)
	}
}
