package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestDirectoryInputCannotOverwriteContainedLog(t *testing.T) {
	root := t.TempDir()
	log := filepath.Join(root, "input.jhlog")
	if err := jhlog.WriteSample(log); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if err := runExport([]string{root, "--out", log}); err == nil {
		t.Fatal("export accepted a contained log as its output")
	}
	actual, err := os.ReadFile(log)
	if err != nil || !bytes.Equal(actual, original) {
		t.Fatalf("contained log changed: %v", err)
	}
}

func TestCommandsAcceptSessionDirectoryAndArchiveInputs(t *testing.T) {
	root := t.TempDir()
	baselineDir := filepath.Join(root, "baseline")
	if err := os.MkdirAll(filepath.Join(baselineDir, "process"), 0o755); err != nil {
		t.Fatal(err)
	}
	baselineLog := filepath.Join(baselineDir, "process", "baseline.jhlog")
	if err := jhlog.WriteSample(baselineLog); err != nil {
		t.Fatal(err)
	}
	candidateLog := filepath.Join(root, "candidate.jhlog")
	if err := jhlog.WriteSample(candidateLog); err != nil {
		t.Fatal(err)
	}
	candidateArchive := filepath.Join(root, "candidate.jhlog.zip")
	writeCommandTestArchive(t, candidateArchive, candidateLog)

	if err := runInspect([]string{baselineDir, "--json"}); err != nil {
		t.Fatalf("inspect directory: %v", err)
	}
	if err := runSize([]string{candidateArchive, "--json"}); err != nil {
		t.Fatalf("size archive: %v", err)
	}
	if err := runExport([]string{baselineDir, "--out", filepath.Join(root, "events.jsonl")}); err != nil {
		t.Fatalf("export directory: %v", err)
	}
	if err := runProblems([]string{candidateArchive, "--format", "json", "--out", filepath.Join(root, "problems.json")}); err != nil {
		t.Fatalf("problems archive: %v", err)
	}
	if err := runCompare([]string{"--baseline", baselineDir, "--candidate", candidateArchive, "--json"}); err != nil {
		t.Fatalf("compare mixed session inputs: %v", err)
	}
	if err := runScorecard([]string{"--baseline", baselineDir, "--candidate", candidateArchive, "--out", filepath.Join(root, "scorecard.json")}); err != nil {
		t.Fatalf("scorecard mixed session inputs: %v", err)
	}
}

func TestRawDirectoryAndArchiveInputsHaveSemanticParity(t *testing.T) {
	root := t.TempDir()
	raw := filepath.Join(root, "raw.jhlog")
	if err := jhlog.WriteSample(raw); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "session", "process")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	directoryLog := filepath.Join(directory, "session.jhlog")
	data, err := os.ReadFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(directoryLog, data, 0o600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "session.jhlog.zip")
	writeCommandTestArchive(t, archive, raw)

	var expected sessionSemanticSignature
	for index, input := range []string{raw, filepath.Dir(directory), archive} {
		resolved, err := openLogInputs([]string{input})
		if err != nil {
			t.Fatal(err)
		}
		summary, err := analyze.InspectFilesWithOptions("session", resolved.Logs, analyze.Options{})
		resolved.Close()
		if err != nil {
			t.Fatal(err)
		}
		actual := semanticSignature(summary)
		if index == 0 {
			expected = actual
		} else if actual != expected {
			t.Fatalf("input %q signature = %+v, want %+v", input, actual, expected)
		}
	}
}

func TestArchiveInputReadsEveryConfigurationEpochLog(t *testing.T) {
	root := t.TempDir()
	first := sessionSelectionPath(root, "2026-09-24", 1, 0, 0)
	second := sessionSelectionPath(root, "2026-09-24", 2, 1, 0)
	writeConfigurationEpochLog(t, first, 1)
	writeConfigurationEpochLog(t, second, 2)
	archivePath := filepath.Join(root, "session-epochs.jhlog.zip")
	archive, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(archive)
	for _, source := range []string{first, second} {
		data, readErr := os.ReadFile(source)
		if readErr != nil {
			t.Fatal(readErr)
		}
		name := "process/" + filepath.Base(source)
		entry, createErr := writer.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := entry.Write(data); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	resolved, err := openLogInputs([]string{archivePath})
	if err != nil {
		t.Fatal(err)
	}
	defer resolved.Close()
	if len(resolved.Logs) != 2 || !resolved.MultiRunEpochs {
		t.Fatalf("merged archive yielded %d logs, multi-run=%v", len(resolved.Logs), resolved.MultiRunEpochs)
	}
	output, err := os.CreateTemp(root, "inspect-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	previousStdout := os.Stdout
	os.Stdout = output
	inspectErr := runInspect([]string{archivePath, "--json"})
	os.Stdout = previousStdout
	if inspectErr != nil {
		t.Fatal(inspectErr)
	}
	if _, err := output.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	var summary struct{ LogCount int }
	if err := json.NewDecoder(output).Decode(&summary); err != nil {
		t.Fatal(err)
	}
	if summary.LogCount != 2 {
		t.Fatalf("inspect of one epoch archive read %d logs, want 2", summary.LogCount)
	}
}

func writeConfigurationEpochLog(t *testing.T, path string, run byte) {
	t.Helper()
	header := jhlog.DefaultSegmentHeader()
	header.RunID[0] = run
	header.SessionID[0] = run
	header.ProcessInstanceID[0] = 1
	header.ProcessName = "main"
	file, _, err := jhlog.CreateWithHeader(path, header)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

type sessionSemanticSignature struct {
	logs, events, http, failed, stalls, contexts, retained, problems int
	records, frames, jank, duration, p95, maxStall                   uint64
}

func semanticSignature(summary analyze.Summary) sessionSemanticSignature {
	return sessionSemanticSignature{
		logs: summary.LogCount, events: summary.EventCount, http: summary.HTTPCount,
		failed: summary.HTTPFailed, stalls: summary.StallCount, contexts: summary.ContextCount,
		retained: int(summary.Retained), problems: len(summary.Problems), records: summary.TotalRecordCount,
		frames: summary.UIFrames, jank: summary.UIJank, duration: summary.DurationMS,
		p95: summary.HTTPP95MS, maxStall: summary.StallMaxMS,
	}
}

func writeCommandTestArchive(t *testing.T, archivePath, logPath string) {
	t.Helper()
	archive, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(archive)
	entry, err := writer.CreateHeader(&zip.FileHeader{Name: "process/candidate.jhlog", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
}
