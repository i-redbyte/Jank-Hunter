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

## Stage 1 native-core host evidence

Release host build, 200,000 timed operations unless noted:

| Benchmark | p50 ns | p95 ns | p99 ns | operations/s | drops |
| --- | ---: | ---: | ---: | ---: | ---: |
| single producer publish + drain | 41 | 42 | 42 | 20,197,940 | 0 |
| multi-producer contention (8 x 100,000) | n/a | n/a | n/a | 6,673,440 | 186,458 |
| full-queue drop | 0* | 42 | 42 | 40,302,267 | 210,000* |
| consumer batch drain, 256 events | 4,875 | 5,209 | 6,542 | 199,918 batches/s | 0 |
| interval pair | 0* | 42 | 42 | 33,642,661 | 0 |
| thread lookup | 0* | 42 | 42 | 40,182,832 | 0 |
| start/stop cycle, 10,000 | 209 | 250 | 250 | 4,129,315 | 0 |

`*` The macOS host clock cannot resolve every sub-clock-tick operation, and the overflow warm-up is
included in the cumulative drop count. Device release gates must use Android tracing/benchmark
clocks and report their own distributions.

Validation completed for this native core:

- release host build with `-Wall -Wextra -Wconversion -Werror`, no exceptions/RTTI;
- multi-producer stress with uniqueness and `drained + dropped == attempted` invariant;
- queue wrap/full, saturating counters, interval orphan/capacity and thread lifecycle tests;
- concurrent publish/stop stress;
- ASan + UBSan host test pass;
- TSan host test pass.

## Stage 2 protocol evidence

| Operation | p50 | p95 | p99 | Notes |
| --- | ---: | ---: | ---: | --- |
| native encode, 256-record batch | 500 ns/batch | 542 ns/batch | 666 ns/batch | 20,000 host iterations, no allocation in encoder |
| Kotlin decode, 256-record batch | 1,088.3 ns/batch | not sampled | not sampled | median of 7 x 10,000 host JVM iterations; 4.3 ns/record |

The Kotlin decoder reuses one mutable record flyweight for the visitor and does not allocate one
event object per record. The benchmark measures decode/visitor dispatch only; it does not include
future canonical persistence objects or Android JNI transition cost.

## Stage 3 ART TI adapter evidence

The Android agent now builds against the official AOSP Android 9 JVMTI declaration, negotiates
GC/monitor capabilities, registers only the requested callback pairs, snapshots thread metadata on
the control path, pairs GC/contention intervals in the generic core, and exposes budgeted triggered
stack capture plus bounded asynchronous method resolution.

Host evidence after the adapter integration:

| Benchmark | p50 ns | p95 ns | p99 ns | operations/s |
| --- | ---: | ---: | ---: | ---: |
| stack fingerprint existing-ID lookup | 0* | 42 | 42 | 42,249,061 |
| single producer publish + drain | 41 | 42 | 42 | 17,217,260 |
| native encode, 256 records | 500 | 542 | 667 | 1,874,041 batches/s |
| start/stop cycle | 167 | 209 | 209 | 5,136,766 |

`*` Same host-clock resolution limitation as the Stage 1 table. The fingerprint table is
fixed-capacity, allocation-free after initialization and performs at most 32 probes.

Verification performed:

- Android debug AAR compiled with NDK 30 for `arm64-v8a` and `x86_64` under
  `-Wall -Wextra -Wconversion -Werror`;
- Kotlin protocol/method-definition unit tests passed;
- release host core tests passed normally, under ASan/UBSan, and under TSan;
- stripped native sizes: 240 KiB arm64-v8a and 220 KiB x86_64; debug AAR: 260 KiB;
- dynamic exports are limited to `Agent_OnAttach`, `Agent_OnLoad`, `Agent_OnUnload`, and the eight
  required JNI bridge functions (in addition to undefined libc imports).

These are build/host results, not device callback-latency claims. Actual ART capability behavior,
startup/frame impact, RSS/PSS and CAUSAL/overload callback percentiles remain `not measured` until
the instrumented device stage.
