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

## Accuracy Scorecard

Use [memory-leak-validation-scorecard.json](memory-leak-validation-scorecard.json) for every real app run. Keep one completed copy per app/version pair and attach the generated Jank Hunter HTML, CLI JSON, LeakCanary export/screenshots, and raw `.jhlog`/`.hprof` artifacts.

Recommended scoring:

| Area | Weight | Target For 9/10 |
| --- | ---: | --- |
| Retained-object recall | 25 | Jank Hunter finds all controlled Activity/Fragment/ViewModel/View leaks that LeakCanary finds, or documents a legitimate scope/timing reason. |
| False-positive control | 20 | Clean object and fixed-flow controls do not become high-severity leaks after retained delay and GC. |
| Heap graph actionability | 20 | Primary path has GC root category, app holder or holder field when present, strong-reference filtering, retained size, and clear first action. |
| Lifecycle coverage | 15 | Activity, Fragment view/binding, ViewModel, View, Service, Dialog, RecyclerView ViewHolder/Adapter flows are covered. |
| Compare stability | 10 | Known introduced leaks are `new`/`worse`; known fixes are `better`/`resolved` under the same scenario cohort. |
| Junior readability | 10 | Reviewer can name owner, suspected field, first fix, and verification step without reading raw HPROF. |

Score interpretation:

- `9-10`: release-candidate leak detection for the tested app class; remaining gaps are edge cases or external validation scale.
- `7-8`: useful but needs targeted tuning before strict CI gates.
- `<7`: do not claim parity with LeakCanary for that app yet; collect false-negative/false-positive examples and add fixtures.

## Controlled Scenario Matrix

Run each scenario at least twice on baseline and candidate. Keep device, network, account state, warm/cold start mode, and retained delay identical.

| Scenario | Expected Result | Must Inspect |
| --- | --- | --- |
| Clean watched object | No high/medium retained leak | False-positive control, GC delay, watcher dedupe |
| Activity held by singleton/cache | Heap-confirmed Activity leak | Static/class root, holder field, retained size |
| Fragment binding not cleared | Fragment view/binding leak | `onDestroyView` watch, binding field, view lifecycle advice |
| ViewModel keeps View/Activity | ViewModel leak after `onCleared` | coroutine/LiveData/reference matcher hints |
| Dialog/listener retains Activity | Dialog/window or callback leak | dismiss/onStop guidance, listener matcher |
| RecyclerView ViewHolder binding retained | ViewHolder/binding leak | recycle/detach watch, adapter/listener cleanup |
| Candidate-only leak burst | `new` or `worse` in compare | chain fingerprint, delta count, delta size |
| Fixed leak in candidate | `better` or `resolved` | stable scenario cohort and same fingerprint |

## Pass Criteria

- Jank Hunter finds the same high-confidence Activity/Fragment/View leaks as LeakCanary on controlled scenarios.
- Jank Hunter compare marks known introduced leaks as `new` or `worse`.
- Fixed leaks become `better` or `resolved` under the same scenario/cohort.
- High severity report rows include a concrete first investigation step.
- Runtime-only rows clearly say when heap evidence is missing.
- Large HPROF files finish with bounded warnings rather than hanging.
- Mathematical compare confidence does not mark tiny, one-off samples as high confidence.
- Heap mode prefers actionable app-owned chains over noisy system-only paths unless retained size is overwhelmingly larger.

## Known Risk Areas

- Native/JNI references require careful interpretation.
- System roots can be noisy without app-owned holder fields.
- Different scenario timing can produce false `resolved` or `new` deltas.
- HPROF parsing is intentionally bounded; very large heaps may produce medium-confidence retained size.
