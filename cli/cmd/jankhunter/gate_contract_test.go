package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestCompareGateEnforcesZeroAndMissingMetrics(t *testing.T) {
	dir := t.TempDir()
	baseline := writeGateHTTPInput(t, dir, "baseline", 100)
	candidate := writeGateHTTPInput(t, dir, "candidate", 200)
	missing := writeGateHTTPInput(t, dir, "missing", 0)
	for _, test := range []struct {
		name, body, candidate string
		gateFailure           bool
	}{
		{"zero", `{"metrics":{"HTTP p95":{"max_regression_abs":0}}}`, candidate, true},
		{"missing", `{"metrics":{"HTTP p95":{"max_regression_abs":1000}}}`, missing, true},
		{"unknown", `{"metrics":{"HTTP p955":{"max_regression_abs":1000}}}`, candidate, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := filepath.Join(dir, test.name+".json")
			if err := os.WriteFile(config, []byte(test.body), 0600); err != nil {
				t.Fatal(err)
			}
			err := runCompare([]string{"--baseline", baseline, "--candidate", test.candidate, "--thresholds", config, "--csv"})
			if err == nil {
				t.Fatal("CI command silently passed")
			}
			var failed gateError
			if test.gateFailure && (!errors.As(err, &failed) || failed.ExitCode() != 1) {
				t.Fatalf("not a CI gate failure: %v", err)
			}
			if !test.gateFailure && !strings.Contains(err.Error(), "unknown metric") {
				t.Fatalf("wrong config error: %v", err)
			}
		})
	}
}

func writeGateHTTPInput(t *testing.T, dir, name string, duration uint64) string {
	t.Helper()
	path := filepath.Join(dir, name+".jhlog")
	file, writer, err := jhlog.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictRoute, ID: 1, Value: "GET /gate"}}); err != nil {
		t.Fatal(err)
	}
	if duration > 0 {
		if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventHTTP, TimeMS: 100, HTTP: &jhlog.HTTPEvent{RouteRef: jhlog.LocalSymbol(1), DurationMS: duration}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
