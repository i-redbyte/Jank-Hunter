# ART TI V1 performance baseline

Measured before ART TI implementation at repository commit `f6b25c6` on 2026-08-12.

## Host environment

- macOS, Apple M5 Max, arm64
- OpenJDK 21.0.10
- Gradle 9.4.1 / AGP 9.0.1
- Go 1.26.3
- installed Android NDK 30.0.14904198

## Existing Kotlin runtime hot paths

Command:

```bash
cd android
./gradlew :jankhunter-runtime:testDebugUnitTest \
  -Djankhunter.benchmark=true \
  -Djankhunter.benchmark.iterations=100000 \
  --tests io.jankhunter.runtime.JankHunterRuntimeBenchmarkTest \
  --no-daemon
```

These are host JVM median sanity measurements, not Android device release gates.

| Operation | ns/op |
| --- | ---: |
| ASM method hook, writer disabled | 0.6 |
| Runnable wrapper creation | 1.4 |
| Runnable wrapper execution | 1.6 |
| Log-spam counter | 1.7 |
| Coroutine propagation wrapper | 2.1 |
| Metric aggregation counter/gauge | 23.6 |
| Flow start/step/end | 144.5 |
| Binary writer counter/gauge | 235.1 |

## Existing Go streaming analysis

Command:

```bash
cd cli
GOCACHE=/private/tmp/jankhunter-go-cache \
  go test ./internal/analyze -run '^$' \
  -bench BenchmarkInspectRepresentative -benchmem -count 3
```

Representative fixture: 51,142 events/op.

| Sample | ns/op | bytes/op | allocs/op |
| --- | ---: | ---: | ---: |
| 1 | 212,420,492 | 451,509,185 | 3,227,334 |
| 2 | 214,815,275 | 451,448,147 | 3,227,354 |
| 3 | 214,253,417 | 451,492,056 | 3,227,339 |

This baseline exposes a high existing report-analysis allocation budget. ART TI aggregation must
stay streaming/bounded and its benchmarks must report incremental cost rather than attributing the
entire pre-existing analyzer cost to the agent.

## Device matrix

No representative Android device run was available during the architecture baseline. The following
cells are intentionally marked `not measured` and must be filled by the end-to-end stage rather
than replaced with estimates:

| Metric | No artifact | Artifact + OFF | CAUSAL | Overload |
| --- | --- | --- | --- | --- |
| startup time | not measured | not measured | not measured | not measured |
| frame timing/jank | not measured | not measured | not measured | not measured |
| process CPU | not measured | not measured | not measured | not measured |
| RSS/PSS/native heap | not measured | not measured | not measured | not measured |
| APK/AAB compressed size | not measured | not measured | not measured | not measured |
| callback p50/p95/p99 | n/a | n/a | not measured | not measured |
| native drops/high-watermark | n/a | n/a | not measured | not measured |
| device energy smoke | not measured | not measured | not measured | not measured |

Battery attribution is not part of V1. Device energy is only an overall overhead smoke metric.
