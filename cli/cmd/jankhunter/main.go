package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
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
