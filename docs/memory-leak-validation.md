# Memory Leak Validation Protocol

This document defines how to validate Jank Hunter leak detection against LeakCanary on a real Android application.

## Goals

- Measure false positives and false negatives against LeakCanary on the same flows.
- Validate that Jank Hunter reports are easier to act on for junior developers.
- Validate compare mode on baseline/candidate app versions.
- Validate that heap mode remains bounded on large HPROF files.

## Required Inputs

- Baseline APK and candidate APK built from comparable commits.
- Debug or QA build with Jank Hunter object watcher enabled.
- LeakCanary enabled in the same build or in a dedicated comparison build.
- A stable scenario list: login, main feed, detail screen, checkout/payment-like flow, background/foreground, rotate/recreate, logout.
- Device matrix: at least one low-RAM device/emulator and one modern device.

## Collection Steps

1. Install baseline APK.
2. Run each scenario from a clean app start.
3. Trigger lifecycle exits for Activity/Fragment screens.
4. Wait at least `retainedObjectDelayMs + 5s`.
5. Call `JankHunter.flush()` or use the sample/debug control.
6. Pull `.jhlog` and optional `.hprof` with `cli/scripts/collect-android-leak-report.sh`.
7. Export LeakCanary result for the same scenario.
8. Repeat for candidate APK.
9. Run `jankhunter compare` with baseline/candidate logs and heap dumps.

## Metrics To Record

- Leak count by scenario.
- New/worse/same/better/resolved leak count.
- Heap-confirmed leak count.
- Runtime-only leak count.
- Top GC root categories.
- Top holder fields.
- Retained size and retained object count.
- Report generation time and peak memory for large HPROF.
- Developer actionability score from review: clear owner, clear fix, clear verification.

## Pass Criteria

- Jank Hunter finds the same high-confidence Activity/Fragment/View leaks as LeakCanary on controlled scenarios.
- Jank Hunter compare marks known introduced leaks as `new` or `worse`.
- Fixed leaks become `better` or `resolved` under the same scenario/cohort.
- High severity report rows include a concrete first investigation step.
- Runtime-only rows clearly say when heap evidence is missing.
- Large HPROF files finish with bounded warnings rather than hanging.

## Known Risk Areas

- Native/JNI references require careful interpretation.
- System roots can be noisy without app-owned holder fields.
- Different scenario timing can produce false `resolved` or `new` deltas.
- HPROF parsing is intentionally bounded; very large heaps may produce medium-confidence retained size.
