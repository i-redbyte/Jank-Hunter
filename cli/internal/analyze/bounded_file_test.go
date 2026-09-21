package analyze

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadThresholdConfigRejectsOversizedInputBeforeDecode(t *testing.T) {
	path := sparseAnalyzeInput(t, "thresholds.json", (1<<20)+1)

	_, err := LoadThresholdConfig(path)
	if err == nil || !strings.Contains(err.Error(), "threshold config size must be between") {
		t.Fatalf("LoadThresholdConfig() error = %v, want bounded size diagnostic", err)
	}
}

func TestLoadJSONHeapEvidenceRejectsOversizedInputBeforeDecode(t *testing.T) {
	path := sparseAnalyzeInput(t, "heap.json", (64<<20)+1)

	_, err := loadJSONHeapEvidence(path)
	if err == nil || !strings.Contains(err.Error(), "heap evidence size must be between") {
		t.Fatalf("loadJSONHeapEvidence() error = %v, want bounded size diagnostic", err)
	}
}

func TestStreamingArtifactLoadersRejectOversizedInputBeforeScan(t *testing.T) {
	tests := []struct {
		name     string
		maxBytes int64
		load     func(string) error
	}{
		{
			name:     "Android component catalog",
			maxBytes: androidComponentCatalogMaxFileBytes,
			load: func(path string) error {
				_, err := LoadAndroidComponentCatalog(path)
				return err
			},
		},
		{
			name:     "DI catalog",
			maxBytes: dependencyInjectionMaxFileBytes,
			load: func(path string) error {
				_, err := LoadDependencyInjectionCatalog(path)
				return err
			},
		},
		{
			name:     "class graph",
			maxBytes: classGraphMaxFileBytes,
			load: func(path string) error {
				_, err := LoadClassGraph(path)
				return err
			},
		},
		{
			name:     "instrumentation diagnostics",
			maxBytes: instrumentationDiagnosticsMaxFileBytes,
			load: func(path string) error {
				_, err := LoadInstrumentationDiagnostics(path)
				return err
			},
		},
		{
			name:     "R8 mapping",
			maxBytes: nameMappingMaxFileBytes,
			load: func(path string) error {
				_, err := LoadNameMapping(path)
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := sparseAnalyzeInput(t, "oversized.input", test.maxBytes+1)
			err := test.load(path)
			if err == nil || !strings.Contains(err.Error(), "exceeds size limit") {
				t.Fatalf("loader error = %v, want bounded size diagnostic", err)
			}
		})
	}
}

func TestLoadThresholdConfigRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "thresholds.json")
	if err := os.WriteFile(path, []byte(`{"min_confidnce":"high"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadThresholdConfig(path)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("LoadThresholdConfig() error = %v, want unknown field diagnostic", err)
	}
}

func TestLoadThresholdConfigRejectsValuesThatWouldDisableOrDistortGate(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "unknown confidence", body: `{"min_confidence":"certain"}`, want: "min_confidence"},
		{name: "negative regression", body: `{"metrics":{"ui_jank_pct":{"max_regression_pct":-1}}}`, want: "must not be negative"},
		{name: "negative leak count", body: `{"leaks":{"max_new":-1}}`, want: "leaks.max_new"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "thresholds.json")
			if err := os.WriteFile(path, []byte(test.body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadThresholdConfig(path)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadThresholdConfig() error = %v, want %q", err, test.want)
			}
		})
	}
}

func sparseAnalyzeInput(t *testing.T, name string, size int64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(size); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
