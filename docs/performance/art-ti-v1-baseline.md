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

## Stage 7 reproducible hardening evidence

`jankhunter_release_performance_budget_v1`

Measured on 2026-08-12. Host: macOS/arm64, Apple M5 Max, AppleClang 21, OpenJDK 21.0.10,
Gradle 9.4.1, AGP 9.0.1, NDK 30. Device smoke: Pixel 10 Pro AVD (`sdk_gphone16k_arm64`), arm64
16 KiB page image, API 37 / Android release 17. Emulator results detect gross regressions; they
are not substitutes for a physical-device release matrix.

### Host callback/core and protocol

Release native core, 200,000 timed operations unless the benchmark defines a count:

| Benchmark | p50 ns | p95 ns | p99 ns | operations/s | drops |
| --- | ---: | ---: | ---: | ---: | ---: |
| publish + drain | 42 | 42 | 42 | 17,197,644 | 0 |
| 8-producer contention | n/a | n/a | n/a | 6,222,193 | 194,976 |
| full queue/drop | 0* | 42 | 42 | 41,244,554 | 210,000* |
| consumer drain, 256 records | 4,875 | 4,917 | 5,167 | 203,800 batches/s | 0 |
| native encode, 256 records | 500 | 542 | 625 | 1,905,813 batches/s | 0 |
| interval/thread/stack lookup | 0* | 42 | 42 | 34–42 million/s | 0 |
| start/stop cycle | 167 | 209 | 209 | 5,219,207 | 0 |

`*` Host clock resolution and overflow warm-up limitations remain as described above. The callback
publish p99 is far below the 10 µs architectural target on this host, but only Android tracing on a
representative physical device can close the device gate.

Kotlin decoder (256 records): 1,031.6 ns/batch and 4.0 ns/record in a previous same-host run. The
bounded Go agent aggregator processes 10,000 events in 431,700–433,836 ns/op with 3,648 B/op and
18 allocs/op. The full pre-existing report pipeline remains much heavier: 51,142 events in
205.3–205.5 ms, about 462.8 MB/op and 3.33 million allocs/op; this is not attributed to ART TI.

Commands:

```bash
cmake -S android/jankhunter-artti/src/main/cpp -B /private/tmp/jh-artti-host \
  -DJH_BUILD_HOST_TESTS=ON -DCMAKE_BUILD_TYPE=Release
cmake --build /private/tmp/jh-artti-host --parallel
/private/tmp/jh-artti-host/jh_artti_core_bench

cd android
./gradlew :jankhunter-artti:testDebugUnitTest \
  -Djankhunter.benchmark=true \
  --tests io.jankhunter.artti.internal.ArtTiNativeProtocolBenchmarkTest

cd ../cli
go test ./internal/analyze -run '^$' \
  -bench BenchmarkAgentAggregatorBoundedStreaming -benchmem -count 3
```

### Device workload, frame, CPU and memory smoke

Each lane uses the same 8-worker workload (20,000 operations/worker), 8 warmups and 40 measured
iterations, then samples 63 frame intervals under eight workload repetitions. Memory samples force
a GC and settle before `Debug.MemoryInfo`; negative PSS delta is valid noise/reclamation, not
negative agent memory.

| Metric | no SDK artifact | SDK + ART TI OFF | CAUSAL |
| --- | ---: | ---: | ---: |
| workload p50 | 702,750 ns | 699,083 ns | 721,375 ns |
| workload p95 | 899,833 ns | 1,035,542 ns | 892,208 ns |
| process CPU delta | 25 ms | 26 ms | 28 ms |
| PSS before / after | 87,831 / 87,010 KiB | 90,746 / 87,577 KiB | 93,043 / 90,018 KiB |
| native heap before / after | 6,108,752 / 6,109,376 B | 6,253,760 / 6,256,848 B | 6,917,504 / 6,920,608 B |
| frame p50 / p95 / max | 16.67 / 16.67 / 16.67 ms | 16.67 / 16.67 / 16.67 ms | 16.67 / 16.67 / 16.67 ms |
| frames over 24 ms | 0 / 63 | 0 / 63 | 0 / 63 |

CAUSAL workload p50 is +3.2% versus OFF and +2.7% versus no-artifact in this run. CPU delta is
only a 25–28 ms coarse sample. A single AVD p95, PSS snapshot or cold start is not a release gate.
One symmetric `am start -W -S` smoke measured 456 ms no-artifact, 484 ms OFF and 506 ms CAUSAL;
multiple physical-device cold/warm cohorts are still required.

Run all three lanes:

```bash
cd android
./gradlew :sample-app:connectedDebugAndroidTest \
  -Pjankhunter.sample.enabled=false \
  -Pandroid.testInstrumentationRunnerArguments.class=io.jankhunter.sample.ArtTiPerformanceSmokeTest
./gradlew :sample-app:connectedDebugAndroidTest \
  -Pjankhunter.sample.artTiMode=OFF \
  -Pandroid.testInstrumentationRunnerArguments.class=io.jankhunter.sample.ArtTiPerformanceSmokeTest
./gradlew :sample-app:connectedDebugAndroidTest \
  -Pjankhunter.sample.artTiMode=CAUSAL \
  -Pandroid.testInstrumentationRunnerArguments.class=io.jankhunter.sample.ArtTiPerformanceSmokeTest
```

### Overload, lifecycle and real callbacks

`ArtTiHardeningTest` passed on the same AVD. It observed capability-negotiated GC, thread
start/end, monitor contention and triggered stack definition/sample events through actual
Android → native callback → Kotlin batch → committed v9 storage. It then attempted 100,000
synthetic events, observed bounded rejection plus queue loss/high-water evidence, verified main
thread responsiveness, and required a clean stopped status. The synthetic producer exists only as
an internal bridge test hook and does not replace the preceding real callback assertions.

Foreground/background transition, secondary-process attachment and stop/restart across the full
API 28–current/OEM matrix are not measured on this single AVD. Non-debuggable behavior is covered
by eligibility/release packaging tests rather than attaching to a release process.

### Binary/APK footprint and publishing

| Artifact | arm64-v8a | x86_64 | total |
| --- | ---: | ---: | ---: |
| stripped release `.so`, uncompressed | 92,944 B | 87,048 B | 179,992 B |
| release AAR entry, deflated | 42,117 B | 39,923 B | 82,040 B |
| complete release AAR | — | — | 149,114 B |

Debug sample APKs were 16,264,108 B for both no-artifact and OFF, and 18,294,461 B for CAUSAL
(+2,030,353 B compressed). This delta is dominated by deliberately retained/unstrippable debug
native symbols; use the stripped release AAR row for production footprint planning. The minified
release sample is 1.5 MB and contains no ART TI library under the default debug-only variant rule.

`scripts/gradle-plugin-smoke.sh` publishes into an isolated Maven repository, builds a Java 17 /
minSdk 23 external Android consumer, runs R8, reuses the configuration cache, verifies both ART TI
ABIs in debug and verifies no ART TI library in release. The release dynamic-symbol audit exposes
only `Agent_OnAttach`, `Agent_OnLoad`, `Agent_OnUnload` and required JNI bridge entries; no STL API
crosses the boundary.

### Sanitizers, fuzzing and remaining gates

- Native normal, ASan+UBSan and TSan host suites pass without suppressions.
- Go canonical agent decoder fuzzing ran 5 seconds / 3,296,468 executions without a failure.
- The native libFuzzer target is present, but Xcode AppleClang 21 cannot link its runtime; CMake
  fails clearly rather than silently skipping. Run it in Linux/LLVM CI with a libFuzzer runtime.
- Android HWASan/ASan was not available in this device infrastructure.
- Device energy is not measured. Battery attribution and battery-specific collectors are outside V1.
- Physical arm64 startup/frame/CPU/PSS and API 28/current multi-OEM coverage remain release-candidate
  gates; unmeasured cells are not treated as zero.
- Exporting the device-created `.jhlog` to the host was intentionally not authorized because it can
  contain app screen/flow/runtime payload. The device test structurally verified the committed log
  in-app. Separately, the full v9 agent golden passed `jankhunter inspect` to JSON and standalone
  HTML (`Agent`, `Findings`, availability/quality/capabilities). This is honest coverage of both
  boundaries, not a claim that a sensitive device payload was pulled.
