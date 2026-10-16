package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/atomicfile"
	"github.com/i-redbyte/jank-hunter/cli/internal/comparisoninput"
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
	baselineReport, remaining, err := takeStringFlag(remaining, "baseline-report", "")
	if err != nil {
		return err
	}
	candidateReport, remaining, err := takeStringFlag(remaining, "candidate-report", "")
	if err != nil {
		return err
	}
	baselineReport = strings.TrimSpace(baselineReport)
	candidateReport = strings.TrimSpace(candidateReport)
	baselineMapping, remaining, err := takeStringFlag(remaining, "baseline-mapping", "")
	if err != nil {
		return err
	}
	candidateMapping, remaining, err := takeStringFlag(remaining, "candidate-mapping", "")
	if err != nil {
		return err
	}
	baselineArtifacts, remaining, err := takeStringFlag(remaining, "baseline-artifacts-dir", "")
	if err != nil {
		return err
	}
	candidateArtifacts, remaining, err := takeStringFlag(remaining, "candidate-artifacts-dir", "")
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
	problemAliasesPath, remaining, err := takeStringFlag(remaining, "problem-aliases", "")
	if err != nil {
		return err
	}
	identityMigrationPath, remaining, err := takeStringFlag(remaining, "identity-migration", "")
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
	if strings.TrimSpace(baselineRaw) != "" && baselineReport != "" {
		return fmt.Errorf("compare accepts only one baseline input: --baseline or --baseline-report")
	}
	if strings.TrimSpace(candidateRaw) != "" && candidateReport != "" {
		return fmt.Errorf("compare accepts only one candidate input: --candidate or --candidate-report")
	}
	baselineInput, err := openLogComma(baselineRaw)
	if err != nil {
		return fmt.Errorf("resolve baseline logs: %w", err)
	}
	defer baselineInput.Close()
	candidateInput, err := openLogComma(candidateRaw)
	if err != nil {
		return fmt.Errorf("resolve candidate logs: %w", err)
	}
	defer candidateInput.Close()
	baselinePaths := baselineInput.Logs
	candidatePaths := candidateInput.Logs
	baselineHeap.resolvedDumps = baselineInput.HeapDumps
	candidateHeap.resolvedDumps = candidateInput.HeapDumps
	if (len(baselinePaths) == 0 && baselineReport == "") || (len(candidatePaths) == 0 && candidateReport == "") {
		return fmt.Errorf("compare needs one baseline and one candidate input; use logs or an inspect HTML report")
	}
	if strings.TrimSpace(problemAliasesPath) != "" && strings.TrimSpace(identityMigrationPath) != "" {
		return fmt.Errorf("compare accepts only one identity mapping: --identity-migration or --problem-aliases")
	}
	outputInputs := []string{thresholdsPath, problemAliasesPath, identityMigrationPath, baselineReport, candidateReport}
	outputInputs = append(outputInputs, resolvedInputPaths(baselineInput)...)
	outputInputs = append(outputInputs, resolvedInputPaths(candidateInput)...)
	if err := rejectOutputInputOverlap(out, outputInputs); err != nil {
		return err
	}
	gateConfig, err := analyze.LoadThresholdConfig(thresholdsPath)
	if err != nil {
		return err
	}
	problemAliases, err := analyze.LoadProblemAliases(problemAliasesPath)
	if err != nil {
		return err
	}
	identityMigration, err := analyze.LoadIdentityMigration(identityMigrationPath)
	if err != nil {
		return err
	}
	builder.outputPath = out
	baselineHeap.outputPath = out
	candidateHeap.outputPath = out
	baselineInputs := resolvedInputPaths(baselineInput)
	candidateInputs := resolvedInputPaths(candidateInput)
	if baselineReport != "" {
		baselineInputs = append(baselineInputs, baselineReport)
	}
	if candidateReport != "" {
		candidateInputs = append(candidateInputs, candidateReport)
	}
	if err := rejectLogInputOverlap("baseline", baselineInputs, "candidate", candidateInputs); err != nil {
		return err
	}
	if baselineReport != "" && (baselineMapping != "" || baselineArtifacts != "" || baselineHeap.dumpRaw != "" || baselineHeap.evidenceRaw != "") {
		return fmt.Errorf("baseline artifact, mapping, and heap options cannot be applied to --baseline-report")
	}
	if candidateReport != "" && (candidateMapping != "" || candidateArtifacts != "" || candidateHeap.dumpRaw != "" || candidateHeap.evidenceRaw != "") {
		return fmt.Errorf("candidate artifact, mapping, and heap options cannot be applied to --candidate-report")
	}
	if err := rejectComparisonHeapInputOverlap(baselineHeap, baselinePaths, candidateHeap, candidatePaths); err != nil {
		return err
	}
	baselineBuilder, candidateBuilder := builder, builder
	if baselineMapping != "" {
		baselineBuilder.mappingPath = baselineMapping
	}
	if candidateMapping != "" {
		candidateBuilder.mappingPath = candidateMapping
	}
	if baselineArtifacts != "" {
		baselineBuilder.artifactsDir = baselineArtifacts
	}
	if candidateArtifacts != "" {
		candidateBuilder.artifactsDir = candidateArtifacts
	}
	var baselineOptions, candidateOptions analyze.Options
	var baseline, candidate analyze.Summary
	if baselineReport != "" {
		baseline, err = comparisoninput.ReadComparisonSnapshot(baselineReport)
	} else {
		baselineOptions, err = baselineBuilder.buildForLogs(baselinePaths)
		if err == nil {
			baselineOptions, err = baselineHeap.apply("baseline", baselinePaths, baselineOptions)
		}
		if err == nil {
			baseline, err = analyze.InspectFilesWithOptions("baseline", baselinePaths, baselineOptions)
		}
	}
	if err != nil {
		return fmt.Errorf("load baseline input: %w", err)
	}
	if candidateReport != "" {
		candidate, err = comparisoninput.ReadComparisonSnapshot(candidateReport)
	} else {
		candidateOptions, err = candidateBuilder.buildForLogs(candidatePaths)
		if err == nil {
			candidateOptions, err = candidateHeap.apply("candidate", candidatePaths, candidateOptions)
		}
		if err == nil {
			candidate, err = analyze.InspectFilesWithOptions("candidate", candidatePaths, candidateOptions)
		}
	}
	if err != nil {
		return fmt.Errorf("load candidate input: %w", err)
	}
	if err := analyze.ValidateIdentityMigration(identityMigration, baseline, candidate); err != nil {
		return fmt.Errorf("validate identity migration: %w", err)
	}
	options := candidateOptions
	if candidateReport != "" {
		options = baselineOptions
	}
	comparison := analyze.CompareWithOptions(baseline, candidate, analyze.ComparisonOptions{ProblemAliases: problemAliases, IdentityMigration: identityMigration})
	gate := analyze.EvaluateGate(comparison, gateConfig)
	if jsonOut {
		if err := writeComparisonJSON(os.Stdout, comparison, gate); err != nil {
			return err
		}
	} else if csvOut {
		if err := writeComparisonCSV(os.Stdout, comparison, gate); err != nil {
			return err
		}
	} else {
		printComparisonScope(os.Stdout, comparison, gate, thresholdsPath != "")
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
		var baselineReports, candidateReports []report.LogReport
		if len(baselinePaths) > 0 {
			baselineReports, err = buildLogReports("baseline", baselinePaths, baselineOptions, baseline)
			if err != nil {
				return err
			}
		}
		if len(candidatePaths) > 0 {
			candidateReports, err = buildLogReports("candidate", candidatePaths, candidateOptions, candidate)
			if err != nil {
				return err
			}
		}
		if err := writeCompareReportSet(out, comparison, baselineReports, candidateReports, baselinePaths, candidatePaths, baselineOptions, candidateOptions, options, reportOptions); err != nil {
			return err
		}
		if !csvOut {
			printReportPath(jsonOut, out)
		}
	}
	if thresholdsPath != "" {
		if gate.Failed {
			return gateError{failures: gate.Failures}
		}
	}
	return nil
}

func printComparisonScope(writer io.Writer, comparison analyze.Comparison, gate analyze.GateResult, gateEnabled bool) {
	scope := comparison.Scope
	fmt.Fprintln(writer, comparisonOutcomeCLIText(comparison.Outcome, scope.Comparability))
	fmt.Fprintln(writer, "Область:", comparisonScopeCLIText(scope))
	fmt.Fprintf(writer, "result=%s scope=%s baseline=%s candidate=%s\n",
		comparison.Outcome,
		scope.Comparability,
		comparisonCoverageText(scope.Baseline),
		comparisonCoverageText(scope.Candidate),
	)
	for _, change := range scope.Changes {
		fmt.Fprintf(writer, "scope metric=%q change=%s eligibility=%s evidence=%s confidence=%s before=%s after=%s reason=%s\n",
			change.Name,
			change.Change,
			change.Eligibility.State,
			change.Evidence,
			change.Confidence,
			optionalFloatText(change.Before),
			optionalFloatText(change.After),
			change.Eligibility.Reason,
		)
	}
	if gateEnabled {
		fmt.Fprintf(writer, "gate=%s\n", gate.Status)
	}
}

func comparisonOutcomeCLIText(outcome analyze.ComparisonChange, comparability analyze.ScenarioComparability) string {
	switch outcome {
	case analyze.ChangeImproved:
		if comparability == analyze.ScenarioPartial {
			return "В сопоставленной части стало лучше"
		}
		return "Стало лучше"
	case analyze.ChangeRegressed:
		if comparability == analyze.ScenarioPartial {
			return "В сопоставленной части стало хуже"
		}
		return "Стало хуже"
	case analyze.ChangeUnchanged:
		return "Заметных изменений не обнаружено"
	case analyze.ChangeMixed:
		return "Результат смешанный"
	case analyze.ChangeNotComparable:
		return "В этих записях разные сценарии"
	default:
		return "Пока недостаточно данных для общего вывода"
	}
}

func comparisonScopeCLIText(scope analyze.ComparisonScope) string {
	switch scope.Comparability {
	case analyze.ScenarioFull:
		return "сценарии сопоставлены полностью"
	case analyze.ScenarioPartial:
		if scope.CommonSubpathGroups > 0 && scope.ExactMatchGroups == 0 {
			return fmt.Sprintf("найдена общая последовательность действий; база %s, проверяемый прогон %s; метрики целых сценариев не сравниваются", comparisonCoverageText(scope.Baseline), comparisonCoverageText(scope.Candidate))
		}
		if scope.CommonSubpathGroups > 0 {
			return fmt.Sprintf("сопоставлена часть сценариев; база %s, проверяемый прогон %s; общих подпутей:%d", comparisonCoverageText(scope.Baseline), comparisonCoverageText(scope.Candidate), scope.CommonSubpathGroups)
		}
		return fmt.Sprintf("сопоставлена часть сценариев; база %s, проверяемый прогон %s", comparisonCoverageText(scope.Baseline), comparisonCoverageText(scope.Candidate))
	case analyze.ScenarioNone:
		return "общей части нет; запишите одинаковый сценарий и повторите сравнение"
	default:
		return "область неизвестна; добавьте метки сценария или используйте новые записи"
	}
}

func comparisonCoverageText(coverage analyze.ScenarioCoverage) string {
	duration := fmt.Sprintf("duration:%d/total:unknown", coverage.MatchedDurationMS)
	if coverage.DurationKnown {
		duration = fmt.Sprintf("duration:%d/%dms", coverage.MatchedDurationMS, coverage.TotalDurationMS)
	}
	if !coverage.TotalKnown {
		return fmt.Sprintf("matched:%d/total:unknown %s", coverage.Matched, duration)
	}
	return fmt.Sprintf("matched:%d/total:%d %s", coverage.Matched, coverage.Total, duration)
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
	baselineInput, err := openLogComma(baselineRaw)
	if err != nil {
		return fmt.Errorf("resolve baseline logs: %w", err)
	}
	defer baselineInput.Close()
	candidateInput, err := openLogComma(candidateRaw)
	if err != nil {
		return fmt.Errorf("resolve candidate logs: %w", err)
	}
	defer candidateInput.Close()
	baselinePaths := baselineInput.Logs
	candidatePaths := candidateInput.Logs
	baselineHeap.resolvedDumps = baselineInput.HeapDumps
	candidateHeap.resolvedDumps = candidateInput.HeapDumps
	if len(baselinePaths) == 0 || len(candidatePaths) == 0 {
		return fmt.Errorf("scorecard needs --baseline and --candidate")
	}
	outputInputs := append(resolvedInputPaths(baselineInput), resolvedInputPaths(candidateInput)...)
	if err := rejectOutputInputOverlap(out, outputInputs); err != nil {
		return err
	}
	builder.outputPath = out
	baselineHeap.outputPath = out
	candidateHeap.outputPath = out
	if err := rejectLogInputOverlap("baseline", resolvedInputPaths(baselineInput), "candidate", resolvedInputPaths(candidateInput)); err != nil {
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
