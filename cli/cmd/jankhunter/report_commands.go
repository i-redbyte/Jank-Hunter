package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/atomicfile"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"github.com/i-redbyte/jank-hunter/cli/internal/mathanalysis"
)

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
