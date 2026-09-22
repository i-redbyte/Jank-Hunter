package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func comparisonMappingFixture(t *testing.T, original string) (string, string) {
	t.Helper()
	directory := t.TempDir()
	mapping := filepath.Join(directory, "mapping.txt")
	data := []byte(original + " -> a:\n")
	if err := os.WriteFile(mapping, data, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	header := jhlog.DefaultSegmentHeader()
	header.BuildIdentity = jhlog.BuildIdentity{State: jhlog.BuildIdentityMapped, MappingSHA256: hex.EncodeToString(digest[:])}
	log := filepath.Join(directory, "run.jhlog")
	file, writer, err := jhlog.CreateWithHeader(log, header)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return log, mapping
}

func TestCompareUsesEachBuildsOwnMapping(t *testing.T) {
	if os.Getenv("JANK_HUNTER_RETRACE_HOME") == "" {
		t.Skip("offline Retrace bundle missing; set JANK_HUNTER_RETRACE_HOME or run make test from cli/")
	}
	baseline, baselineMapping := comparisonMappingFixture(t, "original.Baseline")
	candidate, candidateMapping := comparisonMappingFixture(t, "original.Candidate")
	args := []string{"--baseline", baseline, "--candidate", candidate, "--baseline-mapping", baselineMapping, "--candidate-mapping", candidateMapping}
	if err := runCompare(args); err != nil {
		t.Fatal(err)
	}
	args[len(args)-1] = baselineMapping
	if err := runCompare(args); err == nil || !strings.Contains(err.Error(), "mapping identity mismatch") {
		t.Fatalf("candidate accepted baseline mapping: %v", err)
	}
}
