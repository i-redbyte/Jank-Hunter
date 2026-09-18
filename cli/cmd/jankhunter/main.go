package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

var version = "1.0.11"

const (
	defaultCLIGCPercent              = 75
	defaultCLIMemoryLimitBytes int64 = 512 * 1024 * 1024
)

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

// Keep enough headroom for bounded HPROF and call-graph analysis. A very small GOGC value forces
// continuous collections while the live working set cannot shrink and wastes CPU without reducing
// the actual peak.
func configureCLIGarbageCollector() func() {
	if _, explicit := os.LookupEnv("GOGC"); explicit {
		return func() {}
	}
	previous := debug.SetGCPercent(defaultCLIGCPercent)
	return func() { debug.SetGCPercent(previous) }
}

// The limit is soft: bounded large-input analyses can exceed it briefly. Keeping it near their
// measured working set avoids GC thrashing, while ordinary reports still allocate only what they
// need. Operators retain full control through the standard GOMEMLIMIT environment variable.
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
  jankhunter inspect <logs...> --out report.html [--json] [--presentation] [--animated-background] [--all-sessions] [--artifacts-dir build/generated/jankhunter/<variant>] [--mapping mapping.txt] [--allow-unverified-mapping] [--class-graph class-graph.jsonl] [--instrumentation-diagnostics instrumentation-diagnostics.jsonl] [--di-catalog di-catalog.jsonl] [--android-components-catalog android-components-catalog.jsonl] [--database-evidence database-evidence.json] [--heap-dump heap.hprof] [--heap-evidence heap.json] [--route text] [--screen text] [--owner text] [--class text]
  jankhunter compare --baseline <logs...> --candidate <logs...> --out compare.html [--json|--csv] [--presentation] [--animated-background] [--thresholds thresholds.json] [--artifacts-dir build/generated/jankhunter/<variant>] [--mapping mapping.txt] [--allow-unverified-mapping] [--class-graph class-graph.jsonl] [--instrumentation-diagnostics instrumentation-diagnostics.jsonl] [--di-catalog di-catalog.jsonl] [--android-components-catalog android-components-catalog.jsonl] [--database-evidence database-evidence.json] [--baseline-heap-dump heap.hprof] [--candidate-heap-dump heap.hprof] [--route text] [--screen text] [--owner text] [--class text] [--baseline-mapping mapping.txt] [--candidate-mapping mapping.txt] [--baseline-artifacts-dir dir] [--candidate-artifacts-dir dir]
  jankhunter export <logs...> --out events.jsonl
  jankhunter size <logs...> [--json]
  jankhunter problems <logs...> --out problems.csv [--format csv|json] [--dataset problems|code-problems|leaks|influence|math-findings] [--artifacts-dir build/generated/jankhunter/<variant>] [--mapping mapping.txt] [--allow-unverified-mapping] [--class-graph class-graph.jsonl] [--di-catalog di-catalog.jsonl] [--database-evidence database-evidence.json] [--heap-dump heap.hprof] [--heap-evidence heap.json] [--route text] [--screen text] [--owner text] [--class text]
  jankhunter scorecard --baseline <logs...> --candidate <logs...> [--out scorecard.json] [--artifacts-dir build/generated/jankhunter/<variant>] [--mapping mapping.txt] [--allow-unverified-mapping] [--class-graph class-graph.jsonl] [--instrumentation-diagnostics diagnostics.jsonl] [--di-catalog di-catalog.jsonl] [--android-components-catalog android-components-catalog.jsonl] [--database-evidence database-evidence.json] [--baseline-heap-dump heap.hprof] [--baseline-heap-evidence heap.json] [--candidate-heap-dump heap.hprof] [--candidate-heap-evidence heap.json] [--route text] [--screen text] [--owner text] [--class text]
  jankhunter version

CI thresholds:
  Numeric zero is an active limit; omit a field to leave it unconstrained.
  Unknown metric names, empty threshold files and unavailable requested comparisons fail.
  A global max_severity requires all comparison metrics; select metrics explicitly for partial instrumentation.
  Relative regression from a measured zero baseline requires an absolute threshold.
  leaks.require_heap_for_high needs a verified watched-object association; a class-only HPROF path cannot satisfy it.
  Current JHLOG retention records do not provide that association. Heap class paths and sizes remain separate evidence.
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
