package mathanalysis

import (
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func analyzeInspectForTest(t *testing.T, paths []string, options analyze.Options) (MathReport, error) {
	t.Helper()

	summary, err := analyze.InspectFilesWithOptions(titleFromPaths(paths), paths, options)
	if err != nil {
		return MathReport{}, err
	}
	return AnalyzeInspectWithSummary(paths, options, summary)
}

func analyzeCompareForTest(
	t *testing.T,
	baselinePaths,
	candidatePaths []string,
	options analyze.Options,
) (CompareMathReport, error) {
	t.Helper()

	baselineOptions := options
	candidateOptions := options
	if options.BaselineHeapEvidence != nil {
		baselineOptions.HeapEvidence = options.BaselineHeapEvidence
	}
	if options.CandidateHeapEvidence != nil {
		candidateOptions.HeapEvidence = options.CandidateHeapEvidence
	}

	baselineSummary, err := analyze.InspectFilesWithOptions("baseline", baselinePaths, baselineOptions)
	if err != nil {
		return CompareMathReport{}, err
	}
	candidateSummary, err := analyze.InspectFilesWithOptions("candidate", candidatePaths, candidateOptions)
	if err != nil {
		return CompareMathReport{}, err
	}
	return AnalyzeCompareWithSummaries(baselinePaths, candidatePaths, options, baselineSummary, candidateSummary)
}
