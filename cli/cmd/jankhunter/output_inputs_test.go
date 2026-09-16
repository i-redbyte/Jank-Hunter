package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestOutputCommandsRejectEveryInputAliasBeforeWriting(t *testing.T) {
	commands := []struct {
		name       string
		run        func([]string) error
		comparison bool
	}{
		{"export", runExport, false}, {"inspect", runInspect, false},
		{"problems", runProblems, false}, {"compare", runCompare, true},
		{"scorecard", runScorecard, true},
	}
	for _, command := range commands {
		for _, alias := range []string{"same", "relative", "symlink", "hardlink", "directory-symlink"} {
			t.Run(command.name+"/"+alias, func(t *testing.T) {
				dir := t.TempDir()
				first := filepath.Join(dir, "first.jhlog")
				second := filepath.Join(dir, "second.jhlog")
				for _, p := range []string{first, second} {
					if err := jhlog.WriteSample(p); err != nil {
						t.Fatal(err)
					}
				}
				original, err := os.ReadFile(second)
				if err != nil {
					t.Fatal(err)
				}
				out := second
				switch alias {
				case "relative":
					cwd, cwdErr := os.Getwd()
					if cwdErr != nil {
						t.Fatal(cwdErr)
					}
					out, err = filepath.Rel(cwd, second)
					if err != nil {
						t.Fatal(err)
					}
				case "symlink", "hardlink":
					out = filepath.Join(dir, "output.html")
					link := os.Symlink
					if alias == "hardlink" {
						link = os.Link
					}
					if err := link(second, out); err != nil {
						t.Fatal(err)
					}
				case "directory-symlink":
					if err := os.Symlink(dir, filepath.Join(dir, "alias")); err != nil {
						t.Fatal(err)
					}
					out = filepath.Join(dir, "alias", "second.jhlog")
				}
				before, err := os.Lstat(out)
				if err != nil {
					t.Fatal(err)
				}
				args := []string{first, second, "--out", out}
				if command.comparison {
					args = []string{"--baseline", first, "--candidate", second, "--out", out}
				}
				err = command.run(args)
				if err == nil || !strings.Contains(err.Error(), "output") || !strings.Contains(err.Error(), "input") {
					t.Errorf("expected input/output collision error, got %v", err)
				}
				for _, p := range []string{first, second, out} {
					actual, readErr := os.ReadFile(p)
					if readErr != nil || !bytes.Equal(actual, original) {
						t.Errorf("input bytes changed at %q: %v", p, readErr)
					}
				}
				after, err := os.Lstat(out)
				if err != nil || !os.SameFile(before, after) {
					t.Errorf("output alias was replaced: %v", err)
				}
			})
		}
	}
}

func TestOutputProtectsAuxiliaryInputsAndExcludedSessions(t *testing.T) {
	for _, kind := range []string{"mapping", "heap-evidence", "thresholds", "excluded-session"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			log := filepath.Join(dir, "input.jhlog")
			if err := jhlog.WriteSample(log); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(dir, "auxiliary.json")
			content := []byte("[]\n")
			run := runInspect
			args := []string{log, "--heap-evidence", out, "--out", out}
			switch kind {
			case "mapping":
				content = []byte("com.example.Original -> a:\n")
				args = []string{log, "--mapping", out, "--out", out}
			case "thresholds":
				candidate := copyFileForTest(t, log, filepath.Join(dir, "candidate.jhlog"))
				content = []byte("{}\n")
				run = runCompare
				args = []string{"--baseline", log, "--candidate", candidate, "--thresholds", out, "--out", out}
			case "excluded-session":
				out = sessionSelectionPath(dir, "2026-07-13", 1, 8, 0)
				log = sessionSelectionPath(dir, "2026-07-13", 2, 9, 0)
				writeSessionSelectionLog(t, out, "main", 1, 1, 0)
				writeSessionSelectionLog(t, log, "main", 2, 2, 0)
				var err error
				content, err = os.ReadFile(out)
				if err != nil {
					t.Fatal(err)
				}
				args = []string{out, log, "--out", out}
			}
			if err := os.WriteFile(out, content, 0o600); err != nil {
				t.Fatal(err)
			}
			err := run(args)
			if err == nil || !strings.Contains(err.Error(), "output") || !strings.Contains(err.Error(), "input") {
				t.Errorf("expected input/output collision error, got %v", err)
			}
			actual, readErr := os.ReadFile(out)
			if readErr != nil || !bytes.Equal(actual, content) {
				t.Fatalf("input was overwritten: %v", readErr)
			}
		})
	}
}

func TestBundledReportNeverOverwritesExistingCompanionNamedInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "report-math.html")
	if err := jhlog.WriteSample(input); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "report.html")
	if err := runInspect([]string{input, "--out", out}); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(input)
	if err != nil || !bytes.Equal(actual, original) {
		t.Fatalf("companion input was overwritten: %v", err)
	}
	assertFileContains(t, out, "data-jankhunter-single-html")
}
