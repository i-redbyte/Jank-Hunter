package main

import (
	"fmt"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"github.com/i-redbyte/jank-hunter/cli/internal/report"
)

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
