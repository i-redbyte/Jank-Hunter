# Jank Hunter

Jank Hunter is an Android performance diagnostics toolkit and offline report generator.

It is designed for large legacy Android applications where performance regressions are hard to prove after a release: jank, ANR, slow networking, memory pressure, log/request storms, websocket reconnect loops, and suspected leaks.

The repository is intentionally split into two parts:

- `cli/` - Go command-line utility for parsing `.jhlog` files, comparing builds, exporting data, and producing standalone HTML reports.
- `android/` - Android runtime SDK, optional integrations, and Gradle plugin scaffolding for debug/QA instrumentation.

## Current MVP

The first implementation focuses on:

- compact binary `.jhlog` format with varint records and bit flags;
- streaming-friendly Go parser/writer;
- `jankhunter sample` to generate demo logs;
- `jankhunter inspect` to summarize one or more logs;
- `jankhunter compare` to compare baseline and candidate logs;
- standalone HTML reports with no external CDN dependencies;
- minimal Android runtime scaffold with Kotlin-first collectors, Java-compatible public API, and optional integrations split into separate modules.

## CLI quick start

```bash
cd cli
go test ./...
go run ./cmd/jankhunter sample --out /tmp/sample.jhlog
go run ./cmd/jankhunter inspect /tmp/sample.jhlog --out /tmp/jankhunter-report.html
go run ./cmd/jankhunter compare --baseline /tmp/sample.jhlog --candidate /tmp/sample.jhlog --out /tmp/jankhunter-compare.html
```

## Product principles

- Keep app overhead small: aggregate high-frequency data locally before writing.
- Prefer explicit owner attribution, then cheap generated owner IDs, then sampled stacks.
- Avoid runtime dependency conflicts in host apps.
- Keep release mode opt-in and lightweight; make debug/QA instrumentation richer.
- Make files machine-readable first, then generate human-friendly reports offline.
