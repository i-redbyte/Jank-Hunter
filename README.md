# Jank Hunter

Jank Hunter is an Android performance diagnostics toolkit and offline report generator.

It is designed for large legacy Android applications where performance regressions are hard to prove after a release: jank, ANR, slow networking, memory pressure, log/request storms, websocket reconnect loops, and suspected leaks.

The repository is intentionally split into two main parts:

- `cli/` - Go command-line utility for parsing `.jhlog` files, importing owner maps, comparing builds, exporting data, and producing standalone HTML reports.
- `android/` - Android runtime SDK, optional integrations, sample app, and Gradle plugin for debug/QA bytecode instrumentation.

## Current State

The implementation currently includes:

- compact binary `.jhlog` format with varint records and bit flags;
- tolerant streaming Go parser/writer;
- `jankhunter sample` to generate demo logs;
- `jankhunter inspect` to summarize one or more logs;
- `jankhunter compare` to compare baseline and candidate logs;
- filters by route/screen/owner;
- standalone cyber-styled HTML reports with charts, gauges, subtle animation and no external CDN dependencies;
- bottom-of-report heuristic verdict with findings and recommendations;
- compare HTML reports with baseline/candidate scoreboard plus per-log drill-down sections;
- JSON output and threshold-based CI regression gates in the CLI;
- Android runtime collectors for FPS, stalls, memory, system context, process exits, retained objects, counters, and gauges;
- multi-process runtime policy with per-process `.jhlog` files and process breakdown in CLI reports;
- optional OkHttp/WebSocket integrations;
- optional reflection bridge for AndroidX JankStats;
- Gradle plugin with variant-aware ASM hooks for method counters, OkHttp builder factories, WebSocket listeners, Handler callbacks, Executor/ExecutorService work, and owner-map seed generation;
- runtime `Runnable`/`Callable` owner wrappers that preserve thrown exceptions and Future cancellation behavior;
- CLI owner-map import for resolving generated owner labels in inspect/compare reports;
- Kotlin-only Android sources.

## Checks

```bash
cd cli
go test ./...

cd ../android
./gradlew test assemble --no-daemon
```

CI runs the same core checks on `master`.

Optional connected-device smoke:

```bash
./scripts/android-e2e.sh
```

It runs the sample app instrumented test, pulls `.jhlog`, and generates `reports/android-e2e/report.html`.

Manual sample-app dogfooding:

```bash
./run-sample-app.sh
```

The script starts or reuses an emulator, installs `io.jankhunter.sample`, waits for interactive commands, then pulls `.jhlog` files into `tmp/` and generates a CLI HTML report when you type `log` or `stop`.

## Release

Release and publishing instructions live in [docs/release.md](docs/release.md).
Large-app validation runbook lives in [docs/large-app-validation.md](docs/large-app-validation.md).
Mathematical analysis ideas live in [docs/performance-analysis-math.md](docs/performance-analysis-math.md).

Quick local dry runs:

```bash
cd cli
make release VERSION=0.1.0

cd ../android
./gradlew publishToMavenLocal --no-daemon
```

## CLI quick start

```bash
cd cli
go test ./...
go run ./cmd/jankhunter sample --out /tmp/sample.jhlog
go run ./cmd/jankhunter inspect /tmp/sample.jhlog --out /tmp/jankhunter-report.html
go run ./cmd/jankhunter compare --baseline /tmp/sample.jhlog --candidate /tmp/sample.jhlog --out /tmp/jankhunter-compare.html
go run ./cmd/jankhunter inspect /tmp/sample.jhlog --owner-map android/sample-app/build/generated/jankhunter/debug/owner-map.json
```

The generated HTML files are self-contained dashboards. `inspect` is the quick path from one `.jhlog` or a folder of logs to a detailed report. Long route/screen/owner/context tables are collapsed behind expandable sections, so large logs stay readable. `compare` keeps the first screen focused on deltas and cohort warnings, then embeds expandable details for every baseline and candidate log.

## Product principles

- Keep app overhead small: aggregate high-frequency data locally before writing.
- Prefer explicit owner attribution, then cheap generated owner IDs, then sampled stacks.
- Avoid runtime dependency conflicts in host apps.
- Keep release mode opt-in and lightweight; make debug/QA instrumentation richer.
- Make files machine-readable first, then generate human-friendly reports offline.
