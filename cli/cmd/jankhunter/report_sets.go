package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/mathanalysis"
	"github.com/i-redbyte/jank-hunter/cli/internal/report"
)

func writeInspectReportSet(out string, summary analyze.Summary, paths []string, options analyze.Options, reportOptions report.ReportOptions) error {
	reportOptions.TransientOutput = true
	return writeSingleHTMLReport(out, func(renderPath string) error {
		return writeInspectReportSetUsing(renderPath, summary, paths, options, reportOptions, inspectReportSetWriters{
			primary: report.WriteInspectWithOptions,
			math:    report.WriteMathInspectWithOptions,
		})
	})
}

type inspectPrimaryWriter func(string, analyze.Summary, report.ReportOptions) error

type inspectMathWriter func(string, mathanalysis.MathReport, report.ReportOptions) error

type inspectReportSetWriters struct {
	primary inspectPrimaryWriter
	math    inspectMathWriter
}

func writeInspectReportSetUsing(
	out string,
	summary analyze.Summary,
	paths []string,
	options analyze.Options,
	reportOptions report.ReportOptions,
	writers inspectReportSetWriters,
) error {
	if writers.primary == nil || writers.math == nil {
		return fmt.Errorf("inspect report writers are not configured")
	}
	reportPaths := report.PathsFor(out)
	if reportOptions.GeneratedAt == "" {
		reportOptions.GeneratedAt = time.Now().Format(time.RFC3339)
	}
	companionOptions := reportOptions
	companionOptions.Links = reportPaths.MainLink()
	links := report.ReportLinks{}
	var generationWarnings []string

	if err := report.WriteLeakInspectWithOptions(reportPaths.Leaks, analyze.BuildLeakReport(summary), companionOptions); err != nil {
		generationWarnings = append(generationWarnings, warnReportGeneration("отчет утечек inspect не записан", err))
	} else {
		links.Leaks = filepath.Base(reportPaths.Leaks)
	}
	if summary.Influence.Available {
		if err := report.WriteInfluenceWithOptions(reportPaths.Influence, summary.Influence, "Граф влияния кода", companionOptions); err != nil {
			generationWarnings = append(generationWarnings, warnReportGeneration("граф влияния inspect не записан", err))
		} else {
			links.Influence = filepath.Base(reportPaths.Influence)
		}
	}
	if diagnosticsAvailable(options) {
		if err := report.WriteInstrumentationDiagnosticsWithOptions(
			reportPaths.Diagnostics,
			*options.InstrumentationDiagnostics,
			companionOptions,
		); err != nil {
			generationWarnings = append(generationWarnings, warnReportGeneration("ASM-диагностика inspect не записана", err))
		} else {
			links.Diagnostics = filepath.Base(reportPaths.Diagnostics)
		}
	}
	if dependencyInjectionAvailable(options) {
		if err := report.WriteDependencyInjectionWithOptions(
			reportPaths.DependencyInjection,
			analyze.BuildDependencyInjectionReport(options.DependencyInjectionCatalog, summary),
			companionOptions,
		); err != nil {
			generationWarnings = append(generationWarnings, warnReportGeneration("DI-каталог inspect не записан", err))
		} else {
			links.DependencyInjection = filepath.Base(reportPaths.DependencyInjection)
		}
	}
	mathReport, err := mathanalysis.AnalyzeInspectWithSummary(paths, options, summary)
	if err != nil {
		generationWarnings = append(generationWarnings, warnReportGeneration("математический отчет inspect не создан", err))
	} else {
		mathOptions := companionOptions
		mathOptions.Links.Influence = links.Influence
		if err := writers.math(reportPaths.Math, mathReport, mathOptions); err != nil {
			generationWarnings = append(generationWarnings, warnReportGeneration("математический отчет inspect не записан", err))
		} else {
			links.Math = filepath.Base(reportPaths.Math)
		}
	}
	summary.Warnings = append(append([]string(nil), summary.Warnings...), generationWarnings...)
	reportOptions.Links = links
	return writers.primary(reportPaths.Main, summary, reportOptions)
}

func writeCompareReportSet(
	out string,
	comparison analyze.Comparison,
	baselineReports []report.LogReport,
	candidateReports []report.LogReport,
	baselinePaths []string,
	candidatePaths []string,
	baselineOptions analyze.Options,
	candidateOptions analyze.Options,
	options analyze.Options,
	reportOptions report.ReportOptions,
) error {
	reportOptions.TransientOutput = true
	return writeSingleHTMLReport(out, func(renderPath string) error {
		return writeCompareReportSetFiles(
			renderPath,
			comparison,
			baselineReports,
			candidateReports,
			baselinePaths,
			candidatePaths,
			baselineOptions,
			candidateOptions,
			options,
			reportOptions,
		)
	})
}

func writeCompareReportSetFiles(
	out string,
	comparison analyze.Comparison,
	baselineReports []report.LogReport,
	candidateReports []report.LogReport,
	baselinePaths []string,
	candidatePaths []string,
	baselineOptions analyze.Options,
	candidateOptions analyze.Options,
	options analyze.Options,
	reportOptions report.ReportOptions,
) error {
	reportPaths := report.PathsFor(out)
	if reportOptions.GeneratedAt == "" {
		reportOptions.GeneratedAt = time.Now().Format(time.RFC3339)
	}
	companionOptions := reportOptions
	companionOptions.Links = reportPaths.MainLink()
	links := report.ReportLinks{}
	var generationWarnings []string

	if err := report.WriteLeakCompareWithOptions(reportPaths.Leaks, analyze.BuildLeakCompareReport(comparison), companionOptions); err != nil {
		generationWarnings = append(generationWarnings, warnReportGeneration("отчет утечек compare не записан", err))
	} else {
		links.Leaks = filepath.Base(reportPaths.Leaks)
	}
	if comparison.Candidate.Influence.Available {
		if err := report.WriteInfluenceWithOptions(reportPaths.Influence, comparison.Candidate.Influence, "Граф влияния кода: кандидат", companionOptions); err != nil {
			generationWarnings = append(generationWarnings, warnReportGeneration("граф влияния compare не записан", err))
		} else {
			links.Influence = filepath.Base(reportPaths.Influence)
		}
	}
	if diagnosticsAvailable(options) {
		if err := report.WriteInstrumentationDiagnosticsWithOptions(
			reportPaths.Diagnostics,
			*options.InstrumentationDiagnostics,
			companionOptions,
		); err != nil {
			generationWarnings = append(generationWarnings, warnReportGeneration("ASM-диагностика compare не записана", err))
		} else {
			links.Diagnostics = filepath.Base(reportPaths.Diagnostics)
		}
	}
	if dependencyInjectionAvailable(options) {
		if err := report.WriteDependencyInjectionWithOptions(
			reportPaths.DependencyInjection,
			analyze.BuildDependencyInjectionReport(options.DependencyInjectionCatalog, comparison.Candidate),
			companionOptions,
		); err != nil {
			generationWarnings = append(generationWarnings, warnReportGeneration("DI-каталог compare не записан", err))
		} else {
			links.DependencyInjection = filepath.Base(reportPaths.DependencyInjection)
		}
	}
	mathOptions := options
	mathOptions.BaselineHeapEvidence = baselineOptions.HeapEvidence
	mathOptions.CandidateHeapEvidence = candidateOptions.HeapEvidence
	mathReport, err := mathanalysis.AnalyzeCompareWithSummaries(
		baselinePaths,
		candidatePaths,
		mathOptions,
		comparison.Baseline,
		comparison.Candidate,
	)
	if err != nil {
		generationWarnings = append(generationWarnings, warnReportGeneration("математический отчет compare не создан", err))
	} else {
		mathReportOptions := companionOptions
		mathReportOptions.Links.Influence = links.Influence
		if err := report.WriteMathCompareWithOptions(reportPaths.Math, mathReport, mathReportOptions); err != nil {
			generationWarnings = append(generationWarnings, warnReportGeneration("математический отчет compare не записан", err))
		} else {
			links.Math = filepath.Base(reportPaths.Math)
		}
	}
	comparison.Warnings = append(append([]string(nil), comparison.Warnings...), generationWarnings...)
	reportOptions.Links = links
	return report.WriteCompareReportWithOptions(reportPaths.Main, comparison, baselineReports, candidateReports, reportOptions)
}

func writeSingleHTMLReport(out string, writePages func(string) error) error {
	temporaryDirectory, err := os.MkdirTemp("", "jankhunter-report-")
	if err != nil {
		return fmt.Errorf("create temporary report directory: %w", err)
	}
	defer os.RemoveAll(temporaryDirectory)

	renderPath := filepath.Join(temporaryDirectory, filepath.Base(out))
	if err := writePages(renderPath); err != nil {
		return err
	}
	pages, err := readReportBundlePages(renderPath)
	if err != nil {
		return err
	}
	if err := report.WriteBundle(out, pages); err != nil {
		return fmt.Errorf("write single HTML report: %w", err)
	}
	return nil
}

func readReportBundlePages(mainPath string) ([]report.BundlePage, error) {
	paths := report.PathsFor(mainPath)
	candidates := []struct {
		id       string
		title    string
		path     string
		required bool
	}{
		{id: "overview", title: "Обзор", path: paths.Main, required: true},
		{id: "math", title: "Математический анализ", path: paths.Math},
		{id: "leaks", title: "Утечки памяти", path: paths.Leaks},
		{id: "influence", title: "Граф влияния", path: paths.Influence},
		{id: "diagnostics", title: "ASM диагностика", path: paths.Diagnostics},
		{id: "dependency-injection", title: "DI-каталог", path: paths.DependencyInjection},
	}
	pages := make([]report.BundlePage, 0, len(candidates))
	for _, candidate := range candidates {
		info, err := os.Stat(candidate.path)
		if err != nil {
			if !candidate.required && os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read generated report page %s: %w", candidate.path, err)
		}
		if !info.Mode().IsRegular() || info.Size() <= 0 {
			return nil, fmt.Errorf("generated report page %s is not a non-empty regular file", candidate.path)
		}
		pages = append(pages, report.BundlePage{
			ID:    candidate.id,
			Title: candidate.title,
			Href:  filepath.Base(candidate.path),
			Path:  candidate.path,
		})
	}
	return pages, nil
}

func warnReportGeneration(message string, err error) string {
	warning := fmt.Sprintf("Генерация отчета: %s: %v", message, err)
	fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
	return warning
}
