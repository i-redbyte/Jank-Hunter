# Large App Validation

Status: **not executed in this workspace**.

Reason: this repository contains the Jank Hunter SDK/CLI and a small sample app, but not a large legacy Android application. The validation below is the production runbook and report template to use when a real target app is available. Do not mark this stage complete with pass/fail numbers until the target app has been tested.

## Target App Requirements

Use a debuggable or QA build of a real application that has:

- multiple Activity/Fragment screens;
- real OkHttp traffic;
- at least one WebSocket or long-lived realtime channel if available;
- background work through Executor/coroutines/RxJava or equivalent;
- a representative legacy code path with known performance risk;
- a safe test account and non-production backend.

## Rollout Matrix

Enable Jank Hunter gradually:

| Step | Runtime | OkHttp/WebSocket | Handler/Executor ASM | Method counters | Include packages | Goal |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | on | off | off | off | none | Verify startup, logging, privacy defaults |
| 2 | on | on | off | off | none | Verify network telemetry and route redaction |
| 3 | on | on | on | off | app package | Verify async owner attribution overhead |
| 4 | on | on | on | off | narrow hot package | Verify hook stability in risky flows |
| 5 | on | on | selected | on | one narrow package | Evaluate method-counter noise and cost |

Stop at the first step that causes crashes, ANR risk, unacceptable overhead, or privacy leakage.

## Baseline and Candidate Collection

Collect at least two pools:

```text
baseline/
  device-a/*.jhlog
  device-b/*.jhlog
candidate/
  device-a/*.jhlog
  device-b/*.jhlog
```

Keep cohorts comparable:

- same app version family except the intended candidate change;
- same device tier and SDK where possible;
- same network type;
- same process policy;
- same test script and account state;
- same warm/cold start conditions.

Recommended minimum:

- 5+ logs per cohort;
- 500+ events per cohort;
- at least one run with low-memory/background churn if that is a known production risk.

## Commands

Build and install a local SDK snapshot:

```bash
cd android
./gradlew publishToMavenLocal --no-daemon
```

Pull logs from a debuggable app:

```bash
APP_ID=com.example.legacy
OUT_DIR=reports/large-app/raw
mkdir -p "$OUT_DIR"
adb exec-out run-as "$APP_ID" tar -C files/jankhunter -cf - . | tar -xf - -C "$OUT_DIR"
```

Inspect one pool:

```bash
cd cli
go run ./cmd/jankhunter inspect "../reports/large-app/raw/*.jhlog" \
  --owner-map ../android/app/build/generated/jankhunter/debug/owner-map.json \
  --json \
  --out ../reports/large-app/inspect.html \
  > ../reports/large-app/inspect.json
```

Compare releases:

```bash
go run ./cmd/jankhunter compare \
  --baseline "../reports/large-app/baseline/*.jhlog" \
  --candidate "../reports/large-app/candidate/*.jhlog" \
  --owner-map ../android/app/build/generated/jankhunter/debug/owner-map.json \
  --thresholds ../reports/large-app/thresholds.json \
  --json \
  --out ../reports/large-app/compare.html \
  > ../reports/large-app/compare.json
```

Example gate:

```json
{
  "max_severity": "medium",
  "min_confidence": "medium",
  "metrics": {
    "HTTP p95": {"max_regression_pct": 15},
    "UI jank rate": {"max_regression_abs": 2.0},
    "Main-thread stall max": {"max_regression_pct": 20},
    "Retained objects": {"max_severity": "ok"}
  }
}
```

## Overhead Measurements

Record these before and after each rollout step:

| Area | Measurement | Tooling |
| --- | --- | --- |
| Startup | cold/warm startup time | Android Studio profiler, perfetto, app-specific telemetry |
| CPU | main/background CPU while scenario runs | perfetto or Android Studio profiler |
| Allocations | allocation rate during hot screens | memory profiler |
| Disk | `.jhlog` size and write rate | file size over scenario duration |
| Battery | battery/thermal trend on repeated runs | `dumpsys batterystats`, device thermal status |
| ANR risk | main-thread stalls and blocked input | Jank Hunter stalls + logcat |
| Network | added request latency | compare with integration disabled |

Suggested hard stops:

- crashes or failed startup;
- new ANR/input timeout risk;
- sustained queue drops;
- log growth that exceeds agreed QA budget;
- privacy leakage in routes/owners/process labels.

## Privacy Checklist

Verify generated `.jhlog`, JSON and HTML:

- no URL query strings;
- no request or response headers;
- no request or response bodies;
- no user IDs, emails, tokens, session IDs, or raw UUIDs in routes;
- no object `toString()` values;
- process names are redacted if they reveal internal tenant/customer data;
- owner-map labels contain code/team names only.

Useful local checks:

```bash
rg -n "token|authorization|cookie|email|password|session|user_id" reports/large-app
rg -n "\\?.*=|Bearer|Basic " reports/large-app
```

## Report Template

Fill this table after running the target app.

| Question | Result |
| --- | --- |
| Target app/build | Not run |
| Device/SDK cohorts | Not run |
| Runtime-only startup impact | Not run |
| OkHttp/WebSocket overhead | Not run |
| Handler/Executor overhead | Not run |
| Method-counter overhead | Not run |
| Log size per 10 minutes | Not run |
| Queue drops | Not run |
| Privacy findings | Not run |
| Useful regressions found by CLI | Not run |
| Noisy hooks/defaults to change | Not run |
| Go/no-go recommendation | Blocked until target app is available |

## Current Conclusion

The repository is ready for large-app validation because stages 1-8 provide:

- runtime collectors and multi-process log separation;
- optional network and JankStats integrations;
- ASM owner attribution hooks;
- retained-object diagnostics;
- CLI compare with cohort warnings, JSON, HTML and threshold gate;
- release pipeline and sample app end-to-end test path.

Actual production hardening findings still require a real host application. Until then, the only honest result is: **validation blocked by missing target app, runbook prepared**.
