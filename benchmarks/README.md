# Performance baseline and acceptance

This directory defines the local, reproducible performance contract for Jank Hunter. It deliberately uses generated synthetic data: personal `.jhlog` files, heap dumps, device identifiers, and absolute workstation paths must not be committed.

The runner covers four surfaces:

- Android artifact sizes produced by release/debug assembly;
- runtime hot paths through the opt-in `JankHunterRuntimeBenchmarkTest` suite;
- Go decoder and analyzer benchmarks with allocation metrics;
- end-to-end CLI analysis and HTML report generation, including wall time, peak RSS when the host supports it, and bundle size.

## Capture a reference

Run from the `jank-hunter` root on a quiet machine:

```bash
./scripts/performance-baseline.py capture \
  --out benchmarks/results/reference.json
```

The default `representative` fixture is generated deterministically and models a noisy short session: a large method dictionary, repeated flow attribution, and a high-cardinality runtime graph. Use `--profile smoke` only while iterating on the runner.

Fixture metadata follows the `.jhlog v9` accounting model: semantic data events, dictionary records, and final control records are separate counters. A capture is rejected unless the CLI's decoded semantic-event and dictionary counts exactly match the generated fixture metadata; the minimum values in `acceptance.json` are additional profile-volume guards, not substitutes for that equality check.

For stable comparisons, keep the same machine, JDK, Go version, power mode, profile, iteration counts, and sample counts. Record a reference from the last known-good revision, then record the candidate from the revision under review.

Useful focused captures:

```bash
./scripts/performance-baseline.py capture --skip-android --out benchmarks/results/cli.json
./scripts/performance-baseline.py capture --skip-go-benchmarks --out benchmarks/results/android.json
```

Each result records the enabled capture surfaces (`go_benchmarks`, Android runtime and artifacts, CLI, reports, and peak RSS). A reference and candidate must enable exactly the same surfaces; a focused capture therefore must be compared only with a reference captured with the same skip flags and host RSS capability.

The runner never invokes `detekt`. Android benchmarks are opt-in and filtered to `JankHunterRuntimeBenchmarkTest`; normal unit tests are not part of this command. Their Gradle task is forced to execute on every capture, so an `UP-TO-DATE` result cannot silently remove benchmark measurements from a repeated run.

## Check a candidate

```bash
./scripts/performance-baseline.py check \
  --reference benchmarks/results/reference.json \
  --candidate benchmarks/results/candidate.json
```

[`acceptance.json`](acceptance.json) contains relative regression tolerances and final product ceilings. Every enabled surface must contain measurements, all reference and candidate metric sets must match, and both report groups must contain every required page. Missing values for absolute targets fail as `not measured`.

Peak RSS is disabled only when the host OS is unsupported or `/usr/bin/time` is unavailable. When RSS capture is enabled, every CLI command must report it; missing or unparseable timing output aborts capture instead of silently writing `null`.

The JSON results contain relative command labels and environment versions, not input paths. Raw command output, generated logs, binaries, and reports stay under `benchmarks/results/`, which is ignored by Git.

## Interpreting results

- Compare time and allocation metrics only against a reference captured with the same profile and toolchain.
- Treat a single microbenchmark as a diagnostic, not proof of end-user impact. The end-to-end `inspect_json`, `inspect_report`, and `compare_report` measurements are the release-oriented signals.
- A performance improvement must not weaken quality: the generated fixture must decode completely, report the expected event count, have no partial/corruption/drop warnings, and produce every required report page.
- Device-side CPU, ANR, and frame impact remain a separate final validation on a representative application. This local suite is the fast guardrail that catches regressions before that run.
