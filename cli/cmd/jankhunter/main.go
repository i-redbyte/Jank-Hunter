package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"github.com/i-redbyte/jank-hunter/cli/internal/report"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "sample":
		err = runSample(os.Args[2:])
	case "inspect":
		err = runInspect(os.Args[2:])
	case "compare":
		err = runCompare(os.Args[2:])
	case "export":
		err = runExport(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "jankhunter:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`Jank Hunter CLI

Usage:
  jankhunter sample --out sample.jhlog
  jankhunter inspect <logs...> --out report.html
  jankhunter compare --baseline <logs...> --candidate <logs...> --out compare.html
  jankhunter export <logs...> --out events.jsonl
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
	out, remaining, err := takeStringFlag(args, "out", "")
	if err != nil {
		return err
	}
	paths := expandArgs(remaining)
	if len(paths) == 0 {
		return fmt.Errorf("inspect needs at least one log file")
	}
	logs, err := readLogs(paths)
	if err != nil {
		return err
	}
	summary := analyze.Inspect(strings.Join(paths, ", "), logs)
	printSummary(summary)
	if out != "" {
		if err := report.WriteInspect(out, summary); err != nil {
			return err
		}
		fmt.Printf("report: %s\n", out)
	}
	return nil
}

func runCompare(args []string) error {
	baselineRaw, remaining, err := takeStringFlag(args, "baseline", "")
	if err != nil {
		return err
	}
	candidateRaw, remaining, err := takeStringFlag(remaining, "candidate", "")
	if err != nil {
		return err
	}
	out, _, err := takeStringFlag(remaining, "out", "")
	if err != nil {
		return err
	}
	baselinePaths := expandComma(baselineRaw)
	candidatePaths := expandComma(candidateRaw)
	if len(baselinePaths) == 0 || len(candidatePaths) == 0 {
		return fmt.Errorf("compare needs --baseline and --candidate")
	}
	baselineLogs, err := readLogs(baselinePaths)
	if err != nil {
		return err
	}
	candidateLogs, err := readLogs(candidatePaths)
	if err != nil {
		return err
	}
	comparison := analyze.Compare(
		analyze.Inspect("baseline", baselineLogs),
		analyze.Inspect("candidate", candidateLogs),
	)
	for _, delta := range comparison.Deltas {
		fmt.Printf("%-24s %12s -> %-12s %8s %s\n", delta.Name, delta.Baseline, delta.Candidate, delta.Change, delta.Severity)
	}
	if out != "" {
		if err := report.WriteCompare(out, comparison); err != nil {
			return err
		}
		fmt.Printf("report: %s\n", out)
	}
	return nil
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
	paths := expandArgs(remaining)
	if len(paths) == 0 {
		return fmt.Errorf("export needs at least one log file")
	}
	var writer *os.File
	if out == "" {
		writer = os.Stdout
	} else {
		file, err := os.Create(out)
		if err != nil {
			return err
		}
		defer file.Close()
		writer = file
	}
	for _, path := range paths {
		log, err := jhlog.ReadFile(path)
		if err != nil {
			return err
		}
		if err := jhlog.ExportJSONL(log, writer); err != nil {
			return err
		}
	}
	return nil
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

func readLogs(paths []string) ([]jhlog.Log, error) {
	logs := make([]jhlog.Log, 0, len(paths))
	for _, path := range paths {
		log, err := jhlog.ReadFile(path)
		if err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	return logs, nil
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

func printSummary(summary analyze.Summary) {
	fmt.Printf("logs: %d events: %d duration: %dms\n", summary.LogCount, summary.EventCount, summary.DurationMS)
	fmt.Printf("http: count=%d failed=%d p95=%dms\n", summary.HTTPCount, summary.HTTPFailed, summary.HTTPP95MS)
	fmt.Printf("ui: frames=%d janky=%d rate=%.2f%% avg_fps=%.1f min_fps=%.1f\n", summary.UIFrames, summary.UIJank, summary.UIJankPct, summary.UIAvgFPS, summary.UIMinFPS)
	fmt.Printf("stalls: count=%d max=%dms\n", summary.StallCount, summary.StallMaxMS)
	fmt.Printf("context: samples=%d battery_min=%d%% avail_mem_min=%dKB low_mem=%d rx_max=%d tx_max=%d\n", summary.ContextCount, summary.BatteryMinPct, summary.AvailMemoryMinKB, summary.LowMemoryCount, summary.TrafficRxMax, summary.TrafficTxMax)
	fmt.Printf("memory: max_pss=%dKB retained=%d\n", summary.MemoryMaxKB, summary.Retained)
}
