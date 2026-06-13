package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"github.com/i-redbyte/jank-hunter/cli/internal/report"
)

var version = "dev"

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
	case "version":
		fmt.Println(version)
	case "help", "-h", "--help":
		usage()
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "jankhunter:", err)
		if exit, ok := err.(interface{ ExitCode() int }); ok {
			os.Exit(exit.ExitCode())
		}
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`Jank Hunter CLI

Usage:
  jankhunter sample --out sample.jhlog
  jankhunter inspect <logs...> --out report.html [--json] [--owner-map owner-map.json] [--route text] [--screen text] [--owner text]
  jankhunter compare --baseline <logs...> --candidate <logs...> --out compare.html [--json] [--thresholds thresholds.json] [--owner-map owner-map.json] [--route text] [--screen text] [--owner text]
  jankhunter export <logs...> --out events.jsonl
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
	filter, remaining, err := takeFilterFlags(args)
	if err != nil {
		return err
	}
	ownerMapPath, remaining, err := takeStringFlag(remaining, "owner-map", "")
	if err != nil {
		return err
	}
	jsonOut, remaining, err := takeBoolFlag(remaining, "json")
	if err != nil {
		return err
	}
	out, remaining, err := takeStringFlag(remaining, "out", "")
	if err != nil {
		return err
	}
	paths := expandArgs(remaining)
	if len(paths) == 0 {
		return fmt.Errorf("inspect needs at least one log file")
	}
	ownerMap, err := analyze.LoadOwnerMap(ownerMapPath)
	if err != nil {
		return err
	}
	summary, err := analyze.InspectFilesWithOptions(
		strings.Join(paths, ", "),
		paths,
		analyze.Options{Filter: filter, OwnerMap: ownerMap},
	)
	if err != nil {
		return err
	}
	if jsonOut {
		if err := printJSON(summary); err != nil {
			return err
		}
	} else {
		printSummary(summary)
	}
	if out != "" {
		if err := report.WriteInspect(out, summary); err != nil {
			return err
		}
		printReportPath(jsonOut, out)
	}
	return nil
}

func runCompare(args []string) error {
	filter, remaining, err := takeFilterFlags(args)
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
	ownerMapPath, remaining, err := takeStringFlag(remaining, "owner-map", "")
	if err != nil {
		return err
	}
	jsonOut, remaining, err := takeBoolFlag(remaining, "json")
	if err != nil {
		return err
	}
	thresholdsPath, remaining, err := takeStringFlag(remaining, "thresholds", "")
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
	ownerMap, err := analyze.LoadOwnerMap(ownerMapPath)
	if err != nil {
		return err
	}
	options := analyze.Options{Filter: filter, OwnerMap: ownerMap}
	baseline, err := analyze.InspectFilesWithOptions("baseline", baselinePaths, options)
	if err != nil {
		return err
	}
	candidate, err := analyze.InspectFilesWithOptions("candidate", candidatePaths, options)
	if err != nil {
		return err
	}
	comparison := analyze.Compare(baseline, candidate)
	if jsonOut {
		if err := printJSON(comparison); err != nil {
			return err
		}
	} else {
		for _, warning := range comparison.Warnings {
			fmt.Printf("warning: %s\n", warning)
		}
		for _, delta := range comparison.Deltas {
			fmt.Printf(
				"%-24s %12s -> %-12s %8s %s confidence=%s sample=%d %s\n",
				delta.Name,
				delta.Baseline,
				delta.Candidate,
				delta.Change,
				delta.Severity,
				delta.Confidence,
				delta.SampleSize,
				delta.Interval,
			)
		}
	}
	if out != "" {
		baselineReports, err := buildLogReports("baseline", baselinePaths, options)
		if err != nil {
			return err
		}
		candidateReports, err := buildLogReports("candidate", candidatePaths, options)
		if err != nil {
			return err
		}
		if err := report.WriteCompareReport(out, comparison, baselineReports, candidateReports); err != nil {
			return err
		}
		printReportPath(jsonOut, out)
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

func buildLogReports(group string, paths []string, options analyze.Options) ([]report.LogReport, error) {
	reports := make([]report.LogReport, 0, len(paths))
	for i, path := range paths {
		summary, err := analyze.InspectFilesWithOptions(path, []string{path}, options)
		if err != nil {
			return nil, err
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
	return analyze.Filter{RouteContains: route, ScreenContains: screen, OwnerContains: owner}, remaining, nil
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
	if len(summary.RetainedClasses) > 0 {
		fmt.Printf("retained_classes: %s\n", namedValues(summary.RetainedClasses))
	}
	if len(summary.Owners) > 0 {
		fmt.Printf("top_owners: %s\n", ownerValues(summary.Owners, 5))
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

type gateError struct {
	failures []string
}

func (e gateError) Error() string {
	return "regression gate failed: " + strings.Join(e.failures, "; ")
}

func (e gateError) ExitCode() int {
	return 1
}
