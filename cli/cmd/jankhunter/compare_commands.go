package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/atomicfile"
	"github.com/i-redbyte/jank-hunter/cli/internal/report"
)

func runCompare(args []string) error {
	builder, remaining, err := takeAnalysisOptionsBuilder(args)
	if err != nil {
		return err
	}
	baselineRaw, remaining, err := takeStringFlag(remaining, "baseline", "")
	if err != nil {
		return err
	}
	candidateRaw, remaining, err := takeStringFlag(remaining, "candidate", "")
	if err != nil {
		return err
	}
	baselineHeap, remaining, err := takeHeapInputFlags(remaining, "baseline-heap-dump", "baseline-heap-evidence")
	if err != nil {
		return err
	}
	candidateHeap, remaining, err := takeHeapInputFlags(remaining, "candidate-heap-dump", "candidate-heap-evidence")
	if err != nil {
		return err
	}
	jsonOut, remaining, err := takeBoolFlag(remaining, "json")
	if err != nil {
		return err
	}
	csvOut, remaining, err := takeBoolFlag(remaining, "csv")
	if err != nil {
		return err
	}
	if jsonOut && csvOut {
		return fmt.Errorf("compare accepts only one machine-readable stdout format: --json or --csv")
	}
	presentation, remaining, err := takeBoolFlag(remaining, "presentation")
	if err != nil {
		return err
	}
	animatedBackground, remaining, err := takeBoolFlag(remaining, "animated-background")
	if err != nil {
		return err
	}
	thresholdsPath, remaining, err := takeStringFlag(remaining, "thresholds", "")
	if err != nil {
		return err
	}
	out, remaining, err := takeStringFlag(remaining, "out", "")
	if err != nil {
		return err
	}
	if err := rejectUnexpectedArgs(remaining); err != nil {
		return err
	}
	baselinePaths, err := resolveLogComma(baselineRaw)
	if err != nil {
		return fmt.Errorf("resolve baseline logs: %w", err)
	}
	candidatePaths, err := resolveLogComma(candidateRaw)
	if err != nil {
		return fmt.Errorf("resolve candidate logs: %w", err)
	}
	if len(baselinePaths) == 0 || len(candidatePaths) == 0 {
		return fmt.Errorf("compare needs --baseline and --candidate")
	}
	if err := rejectLogInputOverlap("baseline", baselinePaths, "candidate", candidatePaths); err != nil {
		return err
	}
	if err := rejectComparisonHeapInputOverlap(baselineHeap, baselinePaths, candidateHeap, candidatePaths); err != nil {
		return err
	}
	options, err := builder.buildForLogs(append(append([]string{}, baselinePaths...), candidatePaths...))
	if err != nil {
		return err
	}
	baselineOptions, err := baselineHeap.apply("baseline", baselinePaths, options)
	if err != nil {
		return err
	}
	candidateOptions, err := candidateHeap.apply("candidate", candidatePaths, options)
	if err != nil {
		return err
	}
	baseline, err := analyze.InspectFilesWithOptions("baseline", baselinePaths, baselineOptions)
	if err != nil {
		return err
	}
	candidate, err := analyze.InspectFilesWithOptions("candidate", candidatePaths, candidateOptions)
	if err != nil {
		return err
	}
	comparison := analyze.Compare(baseline, candidate)
	if jsonOut {
		if err := printJSON(comparison); err != nil {
			return err
		}
	} else if csvOut {
		if err := writeComparisonCSV(os.Stdout, comparison); err != nil {
			return err
		}
	} else {
		for _, warning := range comparison.Warnings {
			fmt.Printf("warning: %s\n", warning)
		}
		for _, warning := range comparison.Baseline.Warnings {
			fmt.Printf("warning: baseline: %s\n", warning)
		}
		for _, warning := range comparison.Candidate.Warnings {
			fmt.Printf("warning: candidate: %s\n", warning)
		}
		for _, delta := range comparison.Deltas {
			state := delta.Severity
			if !delta.Comparable {
				state = "не сравнивается"
			}
			basis := strings.TrimSpace(strings.Join([]string{delta.ComparisonNote, delta.Interval}, " "))
			fmt.Printf(
				"%-24s %12s -> %-12s %8s %s доверие=%s выборка=%d %s\n",
				compareCLILabel(delta.Name),
				delta.Baseline,
				delta.Candidate,
				delta.Change,
				state,
				delta.Confidence,
				delta.SampleSize,
				basis,
			)
		}
	}
	if out != "" {
		reportOptions := report.ReportOptions{
			PresentationMode:   presentation,
			AnimatedBackground: animatedBackground,
		}
		baselineReports, err := buildLogReports("baseline", baselinePaths, baselineOptions, baseline)
		if err != nil {
			return err
		}
		candidateReports, err := buildLogReports("candidate", candidatePaths, candidateOptions, candidate)
		if err != nil {
			return err
		}
		if err := writeCompareReportSet(out, comparison, baselineReports, candidateReports, baselinePaths, candidatePaths, baselineOptions, candidateOptions, options, reportOptions); err != nil {
			return err
		}
		if !csvOut {
			printReportPath(jsonOut, out)
		}
	}
	if thresholdsPath != "" {
		config, err := analyze.LoadThresholdConfig(thresholdsPath)
		if err != nil {
			return err
		}
		result := analyze.EvaluateGate(comparison, config)
		if result.Failed {
			return gateError{failures: result.Failures}
		}
	}
	return nil
}

func runScorecard(args []string) error {
	builder, remaining, err := takeAnalysisOptionsBuilder(args)
	if err != nil {
		return err
	}
	baselineRaw, remaining, err := takeStringFlag(remaining, "baseline", "")
	if err != nil {
		return err
	}
	candidateRaw, remaining, err := takeStringFlag(remaining, "candidate", "")
	if err != nil {
		return err
	}
	baselineHeap, remaining, err := takeHeapInputFlags(remaining, "baseline-heap-dump", "baseline-heap-evidence")
	if err != nil {
		return err
	}
	candidateHeap, remaining, err := takeHeapInputFlags(remaining, "candidate-heap-dump", "candidate-heap-evidence")
	if err != nil {
		return err
	}
	out, remaining, err := takeStringFlag(remaining, "out", "")
	if err != nil {
		return err
	}
	if err := rejectUnexpectedArgs(remaining); err != nil {
		return err
	}
	baselinePaths, err := resolveLogComma(baselineRaw)
	if err != nil {
		return fmt.Errorf("resolve baseline logs: %w", err)
	}
	candidatePaths, err := resolveLogComma(candidateRaw)
	if err != nil {
		return fmt.Errorf("resolve candidate logs: %w", err)
	}
	if len(baselinePaths) == 0 || len(candidatePaths) == 0 {
		return fmt.Errorf("scorecard needs --baseline and --candidate")
	}
	if err := rejectLogInputOverlap("baseline", baselinePaths, "candidate", candidatePaths); err != nil {
		return err
	}
	if err := rejectComparisonHeapInputOverlap(baselineHeap, baselinePaths, candidateHeap, candidatePaths); err != nil {
		return err
	}
	options, err := builder.buildForLogs(append(append([]string{}, baselinePaths...), candidatePaths...))
	if err != nil {
		return err
	}
	baselineOptions, err := baselineHeap.apply("baseline", baselinePaths, options)
	if err != nil {
		return err
	}
	candidateOptions, err := candidateHeap.apply("candidate", candidatePaths, options)
	if err != nil {
		return err
	}
	baseline, err := analyze.InspectFilesWithOptions("baseline", baselinePaths, baselineOptions)
	if err != nil {
		return err
	}
	candidate, err := analyze.InspectFilesWithOptions("candidate", candidatePaths, candidateOptions)
	if err != nil {
		return err
	}
	scorecard := analyze.BuildValidationScorecard(
		baselinePaths,
		candidatePaths,
		analyze.Compare(baseline, candidate),
	)
	if out == "" {
		return printJSON(scorecard)
	}
	return atomicfile.Write(out, 0o644, func(file *os.File) error {
		encoder := json.NewEncoder(file)
		encoder.SetIndent("", "  ")
		return encoder.Encode(scorecard)
	})
}

func compareCLILabel(name string) string {
	switch name {
	case "HTTP p95":
		return "HTTP p95"
	case "HTTP failure rate":
		return "Доля HTTP-ошибок"
	case "UI jank rate":
		return "Доля UI-подтормаживаний"
	case "UI avg FPS":
		return "Средний FPS"
	case "Main-thread stall max":
		return "Макс. пауза главного потока"
	case "Max PSS":
		return "Макс. PSS"
	case "Min available memory":
		return "Мин. свободная память"
	case "UID RX delta":
		return "RX UID за прогон"
	case "UID TX delta":
		return "TX UID за прогон"
	case "Retained objects":
		return "Удержанные объекты"
	case "Log spam":
		return "Спам логами"
	case "Problem windows":
		return "Проблемные окна"
	case "Process mix":
		return "Состав процессов"
	case "App version mix":
		return "Состав версий приложения"
	case "SDK mix":
		return "Состав SDK"
	case "Device mix":
		return "Состав устройств"
	case "Network mix":
		return "Состав сетей"
	case "Cohort mix":
		return "Состав когорт"
	default:
		return name
	}
}
