package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

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
		if len(heapDumpPaths) > 1 {
			return options, fmt.Errorf(
				"%s: найдено несколько HPROF рядом с логами (%s); укажите однозначный --heap-dump явно",
				title,
				strings.Join(heapDumpPaths, ", "),
			)
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

func rejectComparisonHeapInputOverlap(
	baseline heapInputFlags,
	baselineLogs []string,
	candidate heapInputFlags,
	candidateLogs []string,
) error {
	baselineDumps, baselineAuto, err := comparisonHeapDumpPaths("baseline", baseline, baselineLogs)
	if err != nil {
		return err
	}
	candidateDumps, candidateAuto, err := comparisonHeapDumpPaths("candidate", candidate, candidateLogs)
	if err != nil {
		return err
	}
	if !baselineAuto && !candidateAuto {
		return nil
	}
	if len(baselineDumps) == 1 && len(candidateDumps) == 1 && baselineDumps[0] == candidateDumps[0] {
		return fmt.Errorf(
			"HPROF %s неоднозначно относится и к baseline, и к candidate; "+
				"передайте --baseline-heap-dump и --candidate-heap-dump явно",
			baselineDumps[0],
		)
	}
	return nil
}

func comparisonHeapDumpPaths(
	title string,
	flags heapInputFlags,
	logs []string,
) ([]string, bool, error) {
	if strings.TrimSpace(flags.evidenceRaw) != "" {
		return nil, false, nil
	}
	if strings.TrimSpace(flags.dumpRaw) != "" {
		paths, err := canonicalizeFileInputs(expandComma(flags.dumpRaw), title+" heap dump")
		return paths, false, err
	}
	paths, err := canonicalizeFileInputs(discoverHeapDumpsNearLogs(logs), title+" auto-discovered heap dump")
	if err != nil {
		return nil, true, err
	}
	if len(paths) > 1 {
		return nil, true, fmt.Errorf(
			"%s: найдено несколько HPROF рядом с логами (%s); укажите --%s-heap-dump явно",
			title,
			strings.Join(paths, ", "),
			title,
		)
	}
	return paths, true, nil
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
