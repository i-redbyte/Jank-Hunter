package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/mathanalysis"
)

func TestMathFindingExportsRejectUnavailableCollectionBeforeWriting(t *testing.T) {
	model := &mathanalysis.MathReport{CollectionLimits: []mathanalysis.CollectionLimit{{Component: "timeline", LimitBytes: 1024, ReservedBytes: 900, RequestedBytes: 200}}}
	for _, format := range []struct {
		name  string
		write func(io.Writer, problemsDataset, analyze.Summary, *mathanalysis.MathReport) error
	}{
		{"json", writeProblemsDatasetJSON}, {"csv", writeProblemsDatasetCSV},
	} {
		t.Run(format.name, func(t *testing.T) {
			var output bytes.Buffer
			err := format.write(&output, datasetMathFindings, analyze.Summary{}, model)
			if err == nil || !strings.Contains(err.Error(), "memory limit") {
				t.Fatalf("missing explicit unavailable-analysis error: %v", err)
			}
			if output.Len() != 0 {
				t.Fatalf("wrote misleading partial dataset: %q", output.String())
			}
		})
	}
}

func TestMathFindingExportsExplainSpectralWorkExhaustion(t *testing.T) {
	model := &mathanalysis.MathReport{CollectionLimits: []mathanalysis.CollectionLimit{{Component: "spectral analysis", Work: &mathanalysis.SpectralWorkLimit{LimitOperations: 100, ConsumedOperations: 80, RequestedOperations: 30}}}}
	for _, write := range []func(io.Writer, problemsDataset, analyze.Summary, *mathanalysis.MathReport) error{writeProblemsDatasetJSON, writeProblemsDatasetCSV} {
		var output bytes.Buffer
		err := write(&output, datasetMathFindings, analyze.Summary{}, model)
		if err == nil || !strings.Contains(err.Error(), "spectral work limit") || output.Len() != 0 {
			t.Fatalf("work exhaustion was misreported or exported partial data: %v", err)
		}
	}
}
