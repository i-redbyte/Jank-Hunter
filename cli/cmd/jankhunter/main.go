package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/atomicfile"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"github.com/i-redbyte/jank-hunter/cli/internal/mathanalysis"
	"github.com/i-redbyte/jank-hunter/cli/internal/report"
)

var version = "1.0.0"

const defaultCLIMemoryLimitBytes int64 = 64 * 1024 * 1024

func main() {
	configureCLIGarbageCollector()
	configureCLIMemoryLimit()
	err := newCommandRegistry(os.Stdout).run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "jankhunter:", err)
		if exit, ok := err.(interface{ ExitCode() int }); ok {
			os.Exit(exit.ExitCode())
		}
		os.Exit(1)
	}
}

// The CLI favors bounded peak memory over retaining a larger heap between collections.
func configureCLIGarbageCollector() func() {
	if _, explicit := os.LookupEnv("GOGC"); explicit {
		return func() {}
	}
	previous := debug.SetGCPercent(35)
	return func() { debug.SetGCPercent(previous) }
}

// The limit is soft: large inputs can exceed it, while the runtime returns unused pages sooner for
// ordinary reports. Operators retain full control through the standard GOMEMLIMIT environment
// variable, matching the GOGC override above.
func configureCLIMemoryLimit() func() {
	if _, explicit := os.LookupEnv("GOMEMLIMIT"); explicit {
		return func() {}
	}
	previous := debug.SetMemoryLimit(defaultCLIMemoryLimitBytes)
	return func() { debug.SetMemoryLimit(previous) }
}

func printVersion(out io.Writer) {
	fmt.Fprintf(out, "Jank Hunter CLI %s\n", version)
	fmt.Fprintf(out, ".jhlog format %s\n", jhlog.FormatVersionString)
}

func usage() {
	fmt.Print(`Jank Hunter CLI

Usage:
  jankhunter sample --out sample.jhlog
  jankhunter inspect <logs...> --out report.html [--json] [--presentation] [--animated-background] [--all-sessions] [--artifacts-dir build/generated/jankhunter/<variant>] [--mapping mapping.txt] [--class-graph class-graph.jsonl] [--instrumentation-diagnostics instrumentation-diagnostics.jsonl] [--di-catalog di-catalog.jsonl] [--android-components-catalog android-components-catalog.jsonl] [--database-evidence database-evidence.json] [--heap-dump heap.hprof] [--heap-evidence heap.json] [--route text] [--screen text] [--owner text] [--class text]
  jankhunter compare --baseline <logs...> --candidate <logs...> --out compare.html [--json|--csv] [--presentation] [--animated-background] [--thresholds thresholds.json] [--artifacts-dir build/generated/jankhunter/<variant>] [--mapping mapping.txt] [--class-graph class-graph.jsonl] [--instrumentation-diagnostics instrumentation-diagnostics.jsonl] [--di-catalog di-catalog.jsonl] [--android-components-catalog android-components-catalog.jsonl] [--database-evidence database-evidence.json] [--baseline-heap-dump heap.hprof] [--candidate-heap-dump heap.hprof] [--route text] [--screen text] [--owner text] [--class text]
  jankhunter export <logs...> --out events.jsonl
  jankhunter size <logs...> [--json]
  jankhunter problems <logs...> --out problems.csv [--format csv|json] [--dataset problems|code-problems|leaks|influence|math-findings] [--artifacts-dir build/generated/jankhunter/<variant>] [--mapping mapping.txt] [--class-graph class-graph.jsonl] [--di-catalog di-catalog.jsonl] [--database-evidence database-evidence.json] [--heap-dump heap.hprof] [--heap-evidence heap.json] [--route text] [--screen text] [--owner text] [--class text]
  jankhunter scorecard --baseline <logs...> --candidate <logs...> [--out scorecard.json] [--artifacts-dir build/generated/jankhunter/<variant>] [--mapping mapping.txt] [--class-graph class-graph.jsonl] [--instrumentation-diagnostics diagnostics.jsonl] [--di-catalog di-catalog.jsonl] [--android-components-catalog android-components-catalog.jsonl] [--database-evidence database-evidence.json] [--baseline-heap-dump heap.hprof] [--baseline-heap-evidence heap.json] [--candidate-heap-dump heap.hprof] [--candidate-heap-evidence heap.json] [--route text] [--screen text] [--owner text] [--class text]
  jankhunter version
`)
}

func runSample(args []string) error {
	out, _, err := takeStringFlag(args, "out", "sample.jhlog")
	if err != nil {
		return err
	}
	if err := jhlog.WriteSample(out); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", out)
	return nil
}

func runInspect(args []string) error {
	builder, remaining, err := takeAnalysisOptionsBuilder(args)
	if err != nil {
		return err
	}
	heap, remaining, err := takeHeapInputFlags(remaining, "heap-dump", "heap-evidence")
	if err != nil {
		return err
	}
	jsonOut, remaining, err := takeBoolFlag(remaining, "json")
	if err != nil {
		return err
	}
	presentation, remaining, err := takeBoolFlag(remaining, "presentation")
	if err != nil {
		return err
	}
	animatedBackground, remaining, err := takeBoolFlag(remaining, "animated-background")
	if err != nil {
		return err
	}
	allSessions, remaining, err := takeBoolFlag(remaining, "all-sessions")
	if err != nil {
		return err
	}
	out, remaining, err := takeStringFlag(remaining, "out", "")
	if err != nil {
		return err
	}
	paths, err := resolveLogArgs(remaining)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("inspect needs at least one log file")
	}
	paths, sessionWarnings := selectLatestSessionLogs(paths, allSessions)
	options, err := builder.buildForLogs(paths)
	if err != nil {
		return err
	}
	options, err = heap.apply(strings.Join(paths, ", "), paths, options)
	if err != nil {
		return err
	}
	summary, err := analyze.InspectFilesWithOptions(
		strings.Join(paths, ", "),
		paths,
		options,
	)
	if err != nil {
		return err
	}
	summary.Warnings = append(sessionWarnings, summary.Warnings...)
	if jsonOut {
		if err := printJSON(summary); err != nil {
			return err
		}
	} else {
		printSummary(summary)
	}
	if out != "" {
		reportOptions := report.ReportOptions{
			PresentationMode:   presentation,
			AnimatedBackground: animatedBackground,
		}
		if err := writeInspectReportSet(out, summary, paths, options, reportOptions); err != nil {
			return err
		}
		printReportPath(jsonOut, out)
	}
	return nil
}

type latestRunCohort struct {
	name   jhlog.SessionLogFilename
	runIDs map[jhlog.ID128]struct{}
}

func selectLatestSessionLogs(paths []string, allSessions bool) ([]string, []string) {
	if allSessions || len(paths) < 2 {
		return paths, nil
	}
	nameByPath := make(map[string]jhlog.SessionLogFilename, len(paths))
	latest := latestRunCohort{}
	hasLatest := false
	for _, path := range paths {
		name, ok := jhlog.ParseSessionLogFilename(path)
		if !ok {
			continue
		}
		nameByPath[path] = name

		switch {
		case !hasLatest || name.CompareSession(latest.name) > 0:
			latest = latestRunCohort{
				name:   name,
				runIDs: map[jhlog.ID128]struct{}{name.RunID: {}},
			}
			hasLatest = true
		case name.CompareSession(latest.name) == 0:
			// Equal canonical keys can occur when files from multiple devices or
			// directories are passed together. Retaining every tied run avoids
			// an input-order-dependent data loss decision.
			latest.runIDs[name.RunID] = struct{}{}
		}
	}
	if !hasLatest {
		return paths, nil
	}
	selected := make([]string, 0, len(paths))
	skipped := make([]string, 0)
	for _, path := range paths {
		name, ok := nameByPath[path]
		if !ok {
			selected = append(selected, path)
			continue
		}
		if _, keep := latest.runIDs[name.RunID]; keep {
			selected = append(selected, path)
			continue
		}
		skipped = append(skipped, path)
	}
	if len(skipped) == 0 {
		return paths, nil
	}
	return selected, []string{
		fmt.Sprintf(
			"Inspect обнаружил несколько Jank Hunter run cohort и по дате и дневному индексу canonical-имени оставил только последнюю целиком; файлы других запусков исключены из отчета: %s. Чтобы анализировать все запуски вместе, передайте --all-sessions.",
			strings.Join(skipped, ", "),
		),
	}
}

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

type analysisOptionsBuilder struct {
	filter               analyze.Filter
	artifactsDir         string
	mappingPath          string
	classGraphPath       string
	diagnosticsPath      string
	diCatalogPath        string
	componentCatalogPath string
	databaseEvidencePath string
	artifactNS           []byte
}

func takeAnalysisOptionsBuilder(args []string) (analysisOptionsBuilder, []string, error) {
	filter, remaining, err := takeFilterFlags(args)
	if err != nil {
		return analysisOptionsBuilder{}, nil, err
	}
	artifactsDir, remaining, err := takeStringFlag(remaining, "artifacts-dir", "")
	if err != nil {
		return analysisOptionsBuilder{}, nil, err
	}
	mappingPath, remaining, err := takeStringFlag(remaining, "mapping", "")
	if err != nil {
		return analysisOptionsBuilder{}, nil, err
	}
	classGraphPath, remaining, err := takeStringFlag(remaining, "class-graph", "")
	if err != nil {
		return analysisOptionsBuilder{}, nil, err
	}
	diagnosticsPath, remaining, err := takeStringFlag(remaining, "instrumentation-diagnostics", "")
	if err != nil {
		return analysisOptionsBuilder{}, nil, err
	}
	diCatalogPath, remaining, err := takeStringFlag(remaining, "di-catalog", "")
	if err != nil {
		return analysisOptionsBuilder{}, nil, err
	}
	componentCatalogPath, remaining, err := takeStringFlag(remaining, "android-components-catalog", "")
	if err != nil {
		return analysisOptionsBuilder{}, nil, err
	}
	databaseEvidencePath, remaining, err := takeStringFlag(remaining, "database-evidence", "")
	if err != nil {
		return analysisOptionsBuilder{}, nil, err
	}
	return analysisOptionsBuilder{
		filter:               filter,
		artifactsDir:         artifactsDir,
		mappingPath:          mappingPath,
		classGraphPath:       classGraphPath,
		diagnosticsPath:      diagnosticsPath,
		diCatalogPath:        diCatalogPath,
		componentCatalogPath: componentCatalogPath,
		databaseEvidencePath: databaseEvidencePath,
	}, remaining, nil
}

func (b analysisOptionsBuilder) build() (analyze.Options, error) {
	return b.buildWithArtifactNamespaces(nil)
}

func (b analysisOptionsBuilder) buildForLogs(paths []string) (analyze.Options, error) {
	namespaces, err := logArtifactNamespaces(paths)
	if err != nil {
		return analyze.Options{}, err
	}
	return b.buildWithArtifactNamespaces(namespaces)
}

func (b analysisOptionsBuilder) buildWithArtifactNamespaces(namespaces map[string]struct{}) (analyze.Options, error) {
	b, err := b.withExplicitArtifactsForNamespaces(namespaces)
	if err != nil {
		return analyze.Options{}, err
	}
	nameMapping, err := analyze.LoadNameMapping(b.mappingPath)
	if err != nil {
		return analyze.Options{}, err
	}
	classGraph, err := analyze.LoadClassGraph(b.classGraphPath)
	if err != nil {
		return analyze.Options{}, err
	}
	diagnostics, err := analyze.LoadInstrumentationDiagnostics(b.diagnosticsPath)
	if err != nil {
		return analyze.Options{}, err
	}
	diCatalog, err := analyze.LoadDependencyInjectionCatalog(b.diCatalogPath)
	if err != nil {
		return analyze.Options{}, err
	}
	componentCatalog, err := analyze.LoadAndroidComponentCatalog(b.componentCatalogPath)
	if err != nil {
		return analyze.Options{}, err
	}
	databaseEvidence, err := analyze.LoadDatabaseEvidence(b.databaseEvidencePath)
	if err != nil {
		return analyze.Options{}, err
	}
	return analyze.Options{
		Filter:                     b.filter,
		ObfuscationMap:             nameMapping,
		ClassGraph:                 classGraph,
		InstrumentationDiagnostics: diagnostics,
		DependencyInjectionCatalog: diCatalog,
		AndroidComponentCatalog:    componentCatalog,
		DatabaseEvidence:           databaseEvidence,
		ArtifactDirectory:          b.artifactsDir,
		ArtifactSymbolNamespace:    append([]byte(nil), b.artifactNS...),
	}, nil
}

type androidArtifactBundle struct {
	directory        string
	metadata         string
	classGraph       string
	diagnostics      string
	diCatalog        string
	componentCatalog string
	symbolNamespace  []byte
}

func (b analysisOptionsBuilder) withExplicitArtifactsForNamespaces(
	namespaces map[string]struct{},
) (analysisOptionsBuilder, error) {
	directory := strings.TrimSpace(b.artifactsDir)
	if directory == "" {
		return b, nil
	}
	bundle, err := loadAndroidArtifactBundle(directory)
	if err != nil {
		return b, err
	}
	if len(namespaces) > 0 && !artifactNamespaceMatches(bundle, namespaces) {
		return b, fmt.Errorf(
			"Jank Hunter --artifacts-dir %q does not match the input .jhlog symbol namespace; rebuild the same app variant or pass its exact artifact directory",
			directory,
		)
	}
	b.artifactsDir = bundle.directory
	classGraphFromBundle := b.classGraphPath == ""
	diagnosticsFromBundle := b.diagnosticsPath == ""
	if b.classGraphPath == "" {
		b.classGraphPath = bundle.classGraph
	}
	if b.diagnosticsPath == "" {
		b.diagnosticsPath = bundle.diagnostics
	}
	if classGraphFromBundle && diagnosticsFromBundle {
		b.artifactNS = append([]byte(nil), bundle.symbolNamespace...)
	}
	if b.diCatalogPath == "" && bundle.diCatalog != "" {
		b.diCatalogPath = bundle.diCatalog
	}
	if b.componentCatalogPath == "" && bundle.componentCatalog != "" {
		b.componentCatalogPath = bundle.componentCatalog
	}
	return b, nil
}

func artifactNamespaceMatches(bundle androidArtifactBundle, namespaces map[string]struct{}) bool {
	if len(namespaces) == 0 {
		return true
	}
	if len(namespaces) != 1 {
		return false
	}
	_, matches := namespaces[string(bundle.symbolNamespace)]
	return matches
}

func loadAndroidArtifactBundle(directory string) (androidArtifactBundle, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return androidArtifactBundle{}, fmt.Errorf("resolve --artifacts-dir %q: %w", directory, err)
	}
	bundle := androidArtifactBundle{
		directory:  absolute,
		metadata:   filepath.Join(absolute, "artifact-metadata.json"),
		classGraph: filepath.Join(absolute, "class-graph.jsonl"),
		diagnostics: filepath.Join(
			absolute,
			"instrumentation-diagnostics.jsonl",
		),
	}
	for _, required := range []struct {
		label string
		path  string
	}{
		{label: "artifact-metadata.json", path: bundle.metadata},
		{label: "class-graph.jsonl", path: bundle.classGraph},
		{label: "instrumentation-diagnostics.jsonl", path: bundle.diagnostics},
	} {
		label := required.label
		path := required.path
		info, statErr := os.Stat(path)
		if statErr != nil || info.IsDir() || info.Size() == 0 {
			return androidArtifactBundle{}, fmt.Errorf(
				"invalid Jank Hunter --artifacts-dir %q: required %s is missing or empty",
				directory,
				label,
			)
		}
	}
	namespace, err := analyze.ReadArtifactMetadataNamespace(bundle.metadata)
	if err != nil {
		return androidArtifactBundle{}, fmt.Errorf("invalid Jank Hunter --artifacts-dir %q: artifact-metadata.json identity cannot be read", directory)
	}
	bundle.symbolNamespace = append([]byte(nil), namespace...)
	diCatalog := filepath.Join(absolute, "di-catalog.jsonl")
	if info, statErr := os.Stat(diCatalog); statErr == nil && !info.IsDir() && info.Size() > 0 {
		bundle.diCatalog = diCatalog
	}
	componentCatalog := filepath.Join(absolute, "android-components-catalog.jsonl")
	if info, statErr := os.Stat(componentCatalog); statErr == nil && !info.IsDir() && info.Size() > 0 {
		bundle.componentCatalog = componentCatalog
	}
	return bundle, nil
}

func logArtifactNamespaces(paths []string) (map[string]struct{}, error) {
	namespaces := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		header, err := jhlog.ReadSessionHeader(path)
		if err != nil {
			return nil, err
		}
		namespaces[string(header.SymbolNamespace)] = struct{}{}
	}
	return namespaces, nil
}

func diagnosticsAvailable(options analyze.Options) bool {
	return options.InstrumentationDiagnostics != nil && options.InstrumentationDiagnostics.Available
}

func dependencyInjectionAvailable(options analyze.Options) bool {
	return options.DependencyInjectionCatalog != nil && options.DependencyInjectionCatalog.Available
}

type heapInputFlags struct {
	dumpRaw     string
	evidenceRaw string
}

func takeHeapInputFlags(args []string, dumpFlag, evidenceFlag string) (heapInputFlags, []string, error) {
	dumpRaw, remaining, err := takeStringFlag(args, dumpFlag, "")
	if err != nil {
		return heapInputFlags{}, nil, err
	}
	evidenceRaw, remaining, err := takeStringFlag(remaining, evidenceFlag, "")
	if err != nil {
		return heapInputFlags{}, nil, err
	}
	return heapInputFlags{dumpRaw: dumpRaw, evidenceRaw: evidenceRaw}, remaining, nil
}

func (h heapInputFlags) apply(title string, paths []string, options analyze.Options) (analyze.Options, error) {
	return optionsWithHeapEvidence(title, paths, options, h.evidenceRaw, h.dumpRaw)
}

func optionsWithHeapEvidence(title string, paths []string, options analyze.Options, heapEvidenceRaw, heapDumpRaw string) (analyze.Options, error) {
	heapEvidencePaths, err := canonicalizeFileInputs(expandComma(heapEvidenceRaw), "heap evidence")
	if err != nil {
		return options, err
	}
	heapDumpPaths, err := canonicalizeFileInputs(expandComma(heapDumpRaw), "heap dump")
	if err != nil {
		return options, err
	}
	autoDiscoveredHeapDumps := false
	if len(heapEvidencePaths) == 0 && len(heapDumpPaths) == 0 {
		heapDumpPaths, err = canonicalizeFileInputs(discoverHeapDumpsNearLogs(paths), "auto-discovered heap dump")
		if err != nil {
			return options, err
		}
		autoDiscoveredHeapDumps = len(heapDumpPaths) > 0
		if !autoDiscoveredHeapDumps {
			return options, nil
		}
	}
	targetClasses := []string{}
	if len(heapDumpPaths) > 0 {
		preliminary, err := analyze.InspectFilesWithOptions(title, paths, options)
		if err != nil {
			return options, err
		}
		targetClasses = analyze.HeapTargetClasses(preliminary)
	}
	heapInputs, err := canonicalizeFileInputs(
		append(append([]string{}, heapEvidencePaths...), heapDumpPaths...),
		"heap input",
	)
	if err != nil {
		return options, err
	}
	evidence, err := analyze.LoadHeapEvidenceFiles(heapInputs, targetClasses)
	if err != nil {
		return options, err
	}
	if autoDiscoveredHeapDumps {
		evidence.Warnings = append(
			[]string{
				fmt.Sprintf(
					"CLI автоматически подключил HPROF рядом с Jank Hunter логами: %s. Чтобы использовать другой дамп памяти, передайте --heap-dump явно.",
					strings.Join(heapDumpPaths, ", "),
				),
			},
			evidence.Warnings...,
		)
	}
	options.HeapEvidence = evidence
	return options, nil
}

func discoverHeapDumpsNearLogs(paths []string) []string {
	logDirs := linkedStringSet{}
	for _, path := range paths {
		if strings.EqualFold(filepath.Ext(path), ".jhlog") {
			logDirs.add(filepath.Dir(path))
		}
	}
	if len(logDirs.values) == 0 {
		return nil
	}
	heapDumps := linkedStringSet{}
	for _, dir := range logDirs.values {
		addHeapDumpMatches(&heapDumps, filepath.Join(dir, "*.hprof"))
		addHeapDumpMatches(&heapDumps, filepath.Join(dir, "heap-dumps", "*.hprof"))
	}
	return heapDumps.values
}

func addHeapDumpMatches(out *linkedStringSet, pattern string) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return
	}
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil || info.IsDir() {
			continue
		}
		out.add(match)
	}
}

type linkedStringSet struct {
	seen   map[string]struct{}
	values []string
}

func (s *linkedStringSet) add(value string) {
	if value == "" {
		return
	}
	if s.seen == nil {
		s.seen = map[string]struct{}{}
	}
	if _, ok := s.seen[value]; ok {
		return
	}
	s.seen[value] = struct{}{}
	s.values = append(s.values, value)
}

func buildLogReports(group string, paths []string, options analyze.Options, combined analyze.Summary) ([]report.LogReport, error) {
	reports := make([]report.LogReport, 0, len(paths))
	for i, path := range paths {
		summary := combined
		if len(paths) != 1 {
			var err error
			summary, err = analyze.InspectFilesWithOptions(path, []string{path}, options)
			if err != nil {
				return nil, err
			}
		}
		reports = append(reports, report.LogReport{
			Name:    path,
			Anchor:  fmt.Sprintf("%s-log-%d", group, i+1),
			Summary: summary,
		})
	}
	return reports, nil
}

func takeFilterFlags(args []string) (analyze.Filter, []string, error) {
	route, remaining, err := takeStringFlag(args, "route", "")
	if err != nil {
		return analyze.Filter{}, nil, err
	}
	screen, remaining, err := takeStringFlag(remaining, "screen", "")
	if err != nil {
		return analyze.Filter{}, nil, err
	}
	owner, remaining, err := takeStringFlag(remaining, "owner", "")
	if err != nil {
		return analyze.Filter{}, nil, err
	}
	className, remaining, err := takeStringFlag(remaining, "class", "")
	if err != nil {
		return analyze.Filter{}, nil, err
	}
	return analyze.Filter{RouteContains: route, ScreenContains: screen, OwnerContains: owner, ClassContains: className}, remaining, nil
}

func runExport(args []string) error {
	out, remaining, err := takeStringFlag(args, "out", "")
	if err != nil {
		return err
	}
	format, remaining, err := takeStringFlag(remaining, "format", "jsonl")
	if err != nil {
		return err
	}
	if format != "jsonl" {
		return fmt.Errorf("unsupported export format %q", format)
	}
	paths, err := resolveLogArgs(remaining)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("export needs at least one log file")
	}
	if out == "" {
		return writeExportEvents(os.Stdout, paths)
	}
	return atomicfile.Write(out, 0o644, func(file *os.File) error {
		return writeExportEvents(file, paths)
	})
}

func writeExportEvents(writer io.Writer, paths []string) error {
	encoder := json.NewEncoder(writer)
	for _, path := range paths {
		err := jhlog.StreamFile(path, func(event jhlog.Event, _ map[uint64]string) error {
			return encoder.Encode(event)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func runSize(args []string) error {
	jsonOut, remaining, err := takeBoolFlag(args, "json")
	if err != nil {
		return err
	}
	paths, err := resolveLogArgs(remaining)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("size needs at least one log file")
	}
	profile, err := jhlog.ProfileFiles(paths)
	if err != nil {
		return err
	}
	if jsonOut {
		return printJSON(profile)
	}
	printSizeProfile(profile)
	return nil
}

func printSizeProfile(profile jhlog.SizeProfile) {
	var totalFileBytes uint64
	var totalBodyBytes uint64
	var totalEvents uint64
	var totalRecords uint64
	var totalDictionary uint64
	var totalControl uint64
	for _, file := range profile.Files {
		totalFileBytes += file.FileBytes
		totalBodyBytes += file.BodyBytes
		totalEvents += file.Events
		totalRecords += file.Records
		totalDictionary += file.Dictionary
		totalControl += file.Control
	}
	fmt.Printf(
		"logs=%d events=%d records=%d dictionary=%d control=%d file=%s body=%s compression=%.1fx\n",
		len(profile.Files),
		totalEvents,
		totalRecords,
		totalDictionary,
		totalControl,
		formatByteSize(totalFileBytes),
		formatByteSize(totalBodyBytes),
		compressionRatio(totalBodyBytes+uint64(len(jhlog.Magic))*uint64(len(profile.Files)), totalFileBytes),
	)
	fmt.Printf("%-14s %10s %10s %10s %8s %7s\n", "type", "events", "bytes", "avg", "body%", "files")
	for _, row := range profile.Types {
		fmt.Printf(
			"%-14s %10d %10s %10.1f %7.1f%% %7d\n",
			row.Name,
			row.Events,
			formatByteSize(row.Bytes),
			row.AvgBytes,
			row.Percent,
			row.Files,
		)
	}
}

func compressionRatio(bodyBytes uint64, fileBytes uint64) float64 {
	if fileBytes == 0 {
		return 0
	}
	return float64(bodyBytes) / float64(fileBytes)
}

func formatByteSize(value uint64) string {
	units := []string{"B", "KB", "MB", "GB"}
	scaled := float64(value)
	unit := 0
	for scaled >= 1024 && unit < len(units)-1 {
		scaled /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%dB", value)
	}
	return fmt.Sprintf("%.1f%s", scaled, units[unit])
}

func runProblems(args []string) error {
	builder, remaining, err := takeAnalysisOptionsBuilder(args)
	if err != nil {
		return err
	}
	heap, remaining, err := takeHeapInputFlags(remaining, "heap-dump", "heap-evidence")
	if err != nil {
		return err
	}
	format, remaining, err := takeStringFlag(remaining, "format", "csv")
	if err != nil {
		return err
	}
	datasetRaw, remaining, err := takeStringFlag(remaining, "dataset", string(datasetProblems))
	if err != nil {
		return err
	}
	dataset, err := parseProblemsDataset(datasetRaw)
	if err != nil {
		return err
	}
	out, remaining, err := takeStringFlag(remaining, "out", "")
	if err != nil {
		return err
	}
	paths, err := resolveLogArgs(remaining)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("problems needs at least one log file")
	}
	options, err := builder.buildForLogs(paths)
	if err != nil {
		return err
	}
	options, err = heap.apply(strings.Join(paths, ", "), paths, options)
	if err != nil {
		return err
	}
	summary, err := analyze.InspectFilesWithOptions(strings.Join(paths, ", "), paths, options)
	if err != nil {
		return err
	}
	var mathReport *mathanalysis.MathReport
	if dataset == datasetMathFindings {
		report, err := mathanalysis.AnalyzeInspectWithSummary(paths, options, summary)
		if err != nil {
			return err
		}
		mathReport = &report
	}
	write := func(writer io.Writer) error {
		switch strings.ToLower(format) {
		case "json":
			return writeProblemsDatasetJSON(writer, dataset, summary, mathReport)
		case "csv":
			return writeProblemsDatasetCSV(writer, dataset, summary, mathReport)
		default:
			return fmt.Errorf("unsupported problems format %q", format)
		}
	}
	if out == "" {
		return write(os.Stdout)
	}
	return atomicfile.Write(out, 0o644, func(file *os.File) error { return write(file) })
}

func takeStringFlag(args []string, name, fallback string) (string, []string, error) {
	long := "--" + name
	short := "-" + name
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == long || arg == short {
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("%s needs a value", long)
			}
			value := args[i+1]
			remaining := append([]string{}, args[:i]...)
			remaining = append(remaining, args[i+2:]...)
			return value, remaining, nil
		}
		if strings.HasPrefix(arg, long+"=") {
			remaining := append([]string{}, args[:i]...)
			remaining = append(remaining, args[i+1:]...)
			return strings.TrimPrefix(arg, long+"="), remaining, nil
		}
	}
	return fallback, args, nil
}

func takeStringFlags(args []string, name string) ([]string, []string, error) {
	long := "--" + name
	short := "-" + name
	values := make([]string, 0, 1)
	remaining := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == long || arg == short:
			if i+1 >= len(args) {
				return nil, nil, fmt.Errorf("%s needs a value", long)
			}
			value := args[i+1]
			if value == "" {
				return nil, nil, fmt.Errorf("%s needs a non-empty value", long)
			}
			values = append(values, value)
			i++
		case strings.HasPrefix(arg, long+"="):
			value := strings.TrimPrefix(arg, long+"=")
			if value == "" {
				return nil, nil, fmt.Errorf("%s needs a non-empty value", long)
			}
			values = append(values, value)
		default:
			remaining = append(remaining, arg)
		}
	}
	return values, remaining, nil
}

func takeBoolFlag(args []string, name string) (bool, []string, error) {
	long := "--" + name
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == long {
			remaining := append([]string{}, args[:i]...)
			remaining = append(remaining, args[i+1:]...)
			return true, remaining, nil
		}
		if strings.HasPrefix(arg, long+"=") {
			value := strings.TrimPrefix(arg, long+"=")
			remaining := append([]string{}, args[:i]...)
			remaining = append(remaining, args[i+1:]...)
			switch value {
			case "1", "true", "yes":
				return true, remaining, nil
			case "0", "false", "no":
				return false, remaining, nil
			default:
				return false, nil, fmt.Errorf("%s expects true or false", long)
			}
		}
	}
	return false, args, nil
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func printReportPath(jsonOut bool, path string) {
	if jsonOut {
		fmt.Fprintf(os.Stderr, "report: %s\n", path)
		return
	}
	fmt.Printf("report: %s\n", path)
}

func expandArgs(args []string) []string {
	var out []string
	for _, arg := range args {
		out = append(out, expandOne(arg)...)
	}
	return out
}

func expandComma(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, expandOne(part)...)
	}
	return out
}

func expandOne(pattern string) []string {
	matches, err := filepath.Glob(pattern)
	if err == nil && len(matches) > 0 {
		return matches
	}
	return []string{pattern}
}

type canonicalLogInput struct {
	path string
	info os.FileInfo
}

func resolveLogArgs(args []string) ([]string, error) {
	if err := rejectUnknownOptions(args); err != nil {
		return nil, err
	}
	return canonicalizeLogInputs(expandArgs(args))
}

func rejectUnexpectedArgs(args []string) error {
	if err := rejectUnknownOptions(args); err != nil {
		return err
	}
	if len(args) > 0 {
		return fmt.Errorf("unexpected argument %s", args[0])
	}
	return nil
}

func rejectUnknownOptions(args []string) error {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return fmt.Errorf("unknown option %s", arg)
		}
	}
	return nil
}

func resolveLogComma(raw string) ([]string, error) {
	return canonicalizeLogInputs(expandComma(raw))
}

func canonicalizeLogInputs(paths []string) ([]string, error) {
	return canonicalizeFileInputs(paths, "log")
}

func canonicalizeFileInputs(paths []string, kind string) ([]string, error) {
	resolved := make([]canonicalLogInput, 0, len(paths))
	for _, input := range paths {
		absolute, err := filepath.Abs(filepath.Clean(input))
		if err != nil {
			return nil, fmt.Errorf("resolve %s path %q: %w", kind, input, err)
		}
		canonical, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return nil, fmt.Errorf("resolve %s path %q: %w", kind, input, err)
		}
		canonical = filepath.Clean(canonical)
		info, err := os.Stat(canonical)
		if err != nil {
			return nil, fmt.Errorf("stat %s path %q: %w", kind, canonical, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s input %q is not a regular file", kind, canonical)
		}
		duplicate := false
		for _, existing := range resolved {
			if os.SameFile(existing.info, info) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		resolved = append(resolved, canonicalLogInput{path: canonical, info: info})
	}
	out := make([]string, len(resolved))
	for index := range resolved {
		out[index] = resolved[index].path
	}
	return out, nil
}

func rejectLogInputOverlap(leftName string, left []string, rightName string, right []string) error {
	leftFiles := make([]canonicalLogInput, 0, len(left))
	for _, path := range left {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat %s log %q: %w", leftName, path, err)
		}
		leftFiles = append(leftFiles, canonicalLogInput{path: path, info: info})
	}
	for _, path := range right {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat %s log %q: %w", rightName, path, err)
		}
		for _, candidate := range leftFiles {
			if os.SameFile(candidate.info, info) {
				return fmt.Errorf(
					"%s and %s log sets overlap: %q and %q refer to the same file; use independent runs for comparison",
					leftName,
					rightName,
					candidate.path,
					path,
				)
			}
		}
	}
	return nil
}

func printSummary(summary analyze.Summary) {
	fmt.Printf(
		"logs: %d data_events: %d data_records: %d total_records: %d dictionary_records: %d control_records: %d duration: %dms\n",
		summary.LogCount,
		summary.EventCount,
		summary.DataRecordCount,
		summary.TotalRecordCount,
		summary.DictionaryRecords,
		summary.ControlRecords,
		summary.DurationMS,
	)
	quality := summary.CollectionQuality
	fmt.Printf("http: count=%d failed=%d p95=%dms\n", summary.HTTPCount, summary.HTTPFailed, summary.HTTPP95MS)
	if summary.UIAvgFPS > 0 {
		fmt.Printf("ui: frames=%d janky=%d rate=%.2f%% avg_fps=%.1f min_fps=%.1f\n", summary.UIFrames, summary.UIJank, summary.UIJankPct, summary.UIAvgFPS, summary.UIMinFPS)
	} else {
		fmt.Printf("ui: frames=%d janky=%d rate=%.2f%% fps=not_measured(%s)\n", summary.UIFrames, summary.UIJank, summary.UIJankPct, summary.UIFPSStatus)
	}
	if len(summary.AppVersions) > 0 {
		fmt.Printf("app_versions: %s\n", namedValues(summary.AppVersions))
	}
	if len(summary.SDKs) > 0 {
		fmt.Printf("sdks: %s\n", namedValues(summary.SDKs))
	}
	if len(summary.Devices) > 0 {
		fmt.Printf("devices: %s\n", namedValues(summary.Devices))
	}
	if len(summary.Cohorts) > 0 {
		fmt.Printf("cohorts: %s\n", namedValues(summary.Cohorts))
	}
	if len(summary.Network) > 0 {
		fmt.Printf("network: %s\n", namedValues(summary.Network))
	}
	if len(summary.JankStats) > 0 {
		fmt.Printf("jankstats: %s\n", namedValues(summary.JankStats))
	}
	fmt.Printf("stalls: count=%d max=%dms\n", summary.StallCount, summary.StallMaxMS)
	if len(summary.Processes) > 0 {
		fmt.Printf("processes: %s\n", namedValues(summary.Processes))
	}
	fmt.Printf("context: samples=%d battery_min=%d%% avail_mem_min=%dKB low_mem=%d rx_max=%d tx_max=%d\n", summary.ContextCount, summary.BatteryMinPct, summary.AvailMemoryMinKB, summary.LowMemoryCount, summary.TrafficRxMax, summary.TrafficTxMax)
	fmt.Printf("memory: max_pss=%dKB retained=%d\n", summary.MemoryMaxKB, summary.Retained)
	inputs := summary.AnalysisInputs
	fmt.Printf(
		"analysis_inputs: status=%s complete=%t runtime=%t class_graph=%t asm_diagnostics=%t heap=%t artifact_identity=%t auto_discovered=%t artifacts=%q missing=%s\n",
		inputs.Status,
		inputs.Complete,
		inputs.RuntimeEvidence,
		inputs.ClassGraph,
		inputs.InstrumentationDiagnostics,
		inputs.HeapEvidence,
		inputs.ArtifactIdentityVerified,
		inputs.ArtifactsAutoDiscovered,
		inputs.ArtifactDirectory,
		strings.Join(inputs.Missing, ","),
	)
	if len(summary.RetainedClasses) > 0 {
		fmt.Printf("retained_classes: %s\n", namedValues(summary.RetainedClasses))
	}
	if len(summary.Owners) > 0 {
		fmt.Printf("top_owners: %s\n", ownerValues(summary.Owners, 5))
	}
	fmt.Printf(
		"collection_quality: level=%s complete=%t chain_valid=%t process_scope=%s all_processes=%t process_roster=%d/%d roster_complete=%t roster_declared=%t run_cohorts=%d cohort_consistent=%t counters_valid=%t quality_progression=%t sealed=%d unsealed=%d accepted=%d written=%d committed_chunks=%d/%d runtime_graph=%d/%d/%d known_lost=%d hook_failures=%d dictionary_overflow=%d dictionary_truncated=%d\n",
		quality.Level,
		quality.Complete,
		quality.ChainValid,
		quality.ProcessScope,
		quality.AllProcessesConfigured,
		quality.ObservedProcessCount,
		quality.ExpectedProcessCount,
		quality.ProcessRosterComplete,
		quality.ProcessRosterDeclarationComplete,
		quality.RunCohortCount,
		quality.RunCohortConsistent,
		quality.CounterInvariantsValid,
		quality.QualityProgressionValid,
		quality.SealedSegments,
		quality.UnsealedSegments,
		quality.AcceptedEvents,
		quality.WrittenEvents,
		quality.ReportedCommittedChunks,
		quality.DecodedCommittedChunks,
		quality.DecodedRuntimeGraphCalls,
		quality.RuntimeGraphEmittedEvents,
		quality.RuntimeGraphInputEvents,
		quality.KnownLostEvents,
		quality.RuntimeHookFailures,
		quality.DictionaryOverflow,
		quality.DictionaryTruncated,
	)
	if len(quality.Notices) > 0 || len(summary.Warnings) > 0 {
		fmt.Println("collection_quality_details:")
		for _, notice := range quality.Notices {
			fmt.Printf("info: Качество сбора: %s.\n", notice)
		}
		for _, warning := range summary.Warnings {
			fmt.Printf("warning: %s\n", warning)
		}
	}
	for _, detail := range quality.RuntimeHookFailureDetails {
		fmt.Printf("hook_failure: reason=%s count=%d impact=%s explanation=%s\n", detail.Reason, detail.Count, detail.Impact, detail.Explanation)
	}
}

func namedValues(values []analyze.NamedValue) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%s=%d", value.Name, value.Value))
	}
	return strings.Join(parts, ", ")
}

func ownerValues(values []analyze.OwnerStats, limit int) string {
	if len(values) < limit {
		limit = len(values)
	}
	parts := make([]string, 0, limit)
	for _, value := range values[:limit] {
		parts = append(parts, fmt.Sprintf("%s=%d max=%dms", value.Owner, value.Count, value.MaxMS))
	}
	return strings.Join(parts, ", ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

type gateError struct {
	failures []string
}

func (e gateError) Error() string {
	return "regression gate failed: " + strings.Join(e.failures, "; ")
}

func (e gateError) ExitCode() int {
	return 1
}
