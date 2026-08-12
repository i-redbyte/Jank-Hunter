# Battery runtime attribution backlog

Status: research backlog; explicitly not part of ART TI V1.

This document does not authorize implementation. ART TI V1 adds no battery collector, battery DSL,
battery-specific thread, timer, platform read, hook, or event. The repository already had coarse
device/battery context in the optional `SystemContextSampler` before ART TI work; that context is
not energy attribution and must never be presented as such. A future battery feature needs its own
review, capability contract, performance/privacy budget, experiment design, and opt-in rollout.

## Product hypothesis

Jank Hunter could combine ART TI stack/thread/GC evidence, native per-thread CPU sampling, Android
UID health signals, application flow/screen/owner context, and controlled device energy counters to
form testable hypotheses about runtime events and code regions that may materially contribute to
battery drain.

It must not promise exact mWh per method unless a hardware/system source directly supports that
claim. Most available signals support correlation and constrained attribution, not direct
method-level energy measurement.

## Five distinct inference layers

Reports and schemas must preserve these layers instead of collapsing them into one score:

1. **Device energy measurement.** A device-level current, charge, energy, or external power trace,
   with units, sample interval, counter semantics, calibration and supported/unsupported state.
2. **Application/UID activity attribution.** Evidence that process/UID CPU, wakelocks, jobs,
   network, sensors or radios were active in the measured window. This does not isolate the screen,
   thermal system, modem tail, other apps or OS work.
3. **Thread/stack CPU attribution.** Per-thread CPU deltas joined to stable Jank Hunter thread tokens,
   Linux TIDs and sampled stacks. CPU time is not energy and stack sampling is incomplete evidence.
4. **Temporal correlation.** Clock-calibrated overlap among app context, runtime work, UID activity,
   thermal/network/charging state and device energy change.
5. **Causal hypothesis.** A falsifiable explanation with confidence, missing evidence, confounders,
   alternative explanations and a proposed controlled experiment.

Every finding must expose its highest supported layer. A temporal correlation must not be rendered
as a causal conclusion, and a device energy delta must not be silently assigned to one method.

## Candidate public evidence sources

All sources are capability-checked at runtime and represented as `available`, `partial`,
`permission_denied`, `unsupported`, `invalid_counter`, or `not_collected`; missing values are never
encoded as zero.

| Source | Potential evidence | Principal uncertainty / required check |
| --- | --- | --- |
| ART TI | stable thread identity, triggered stacks, GC and contention intervals | ART/JVMTI capability and metadata availability vary by API/device; sampling coverage is bounded |
| Native sampler | `/proc/self/task/<tid>` CPU deltas and process counters | file visibility, TID reuse, clock ticks, task exit races, read cost and OEM/kernel behavior |
| `SystemHealthManager` / `UidHealthStats` | UID CPU, wakelocks, jobs, sensors, Wi-Fi/Bluetooth and other exposed health timers/counters | API/OEM field support, permissions, counter units, reset/wrap behavior and aggregation interval |
| `BatteryManager` | device current/charge/energy properties | counter availability, sign convention, resolution, update cadence, charging behavior and whether the value is cumulative or instantaneous |
| App SDK / ASM context | screen, flow, step, owner, scheduled work and public call-site markers | semantic quality depends on app instrumentation; high-cardinality labels require bounds/privacy rules |
| Perfetto / Macrobenchmark / Power Profiler | controlled timing, scheduler, CPU and device-specific power evidence | normally offline/QA-only; trace setup and device support differ and production overhead is unacceptable |
| External hardware monitor | calibrated device power reference | lab-only, device wiring and automation complexity; strongest energy evidence when properly calibrated |

Possible public call-site hooks are a fallback when aggregate evidence cannot distinguish logically
different work. They must be explicit, bounded and avoid payload values. No wakelock, network,
sensor or framework hook should be added before aggregate/public system evidence is evaluated.

## Example hypothesis chain

```text
background image prefetch
  -> repeated worker activations
  -> dominant decode/resize stacks on one worker thread
  -> high per-thread CPU delta plus GC pressure
  -> partial wakelock and Wi-Fi activity in the same calibrated window
  -> device energy delta accelerates relative to a controlled baseline
  -> hypothesis: prefetch policy may materially increase drain
  -> alternatives: screen brightness, radio tail, thermal throttling, another UID, cache state
  -> validation: disable only prefetch and repeat randomized baseline/candidate trials
```

The first four arrows can be measured or temporally joined. The final attribution remains a
hypothesis until controlled repetitions reject plausible alternatives.

## Controlled experiment and calibration plan

### Cohort controls

Baseline and candidate runs must align, record or stratify at least:

- physical device, build fingerprint, API/security patch and battery age/health where observable;
- app/build/config hash, account/data/cache state and scenario script;
- charging state, initial state-of-charge band and elapsed stabilization time after unplug;
- display state/brightness/refresh rate and foreground/background state;
- thermal status and pre-run cool-down policy;
- network transport, signal conditions, VPN and server/cache behavior;
- other foreground/background apps, sync/jobs and device idle/power-save state;
- run duration, warm/cold start and randomized baseline/candidate order.

Each cohort needs repeated trials. Report distributions and paired/robust deltas rather than one
trace. Device-energy claims require a minimum signal-to-noise criterion established per source and
device; a counter below resolution is `insufficient_signal`, not `0 mWh`.

### Clock and counter calibration

- Calibrate every source to a monotonic clock with midpoint and uncertainty, using the existing ART
  TI clock-sync approach where possible.
- Record counter kind (`monotonic`, `delta`, `instantaneous`), units, resolution, nominal cadence,
  reset/wrap policy and source-specific validity flags.
- Detect process/thread start and TID reuse; join Linux TID only inside its recorded lifetime and
  use the stable thread token as the session identity.
- Treat long sampling gaps, suspend, clock uncertainty and counter reset as explicit data gaps.
- Establish idle/control fixtures and known CPU/network workloads on each validation device before
  assigning confidence to a production scenario.

## Future evidence model

The semantic model should remain storage-neutral and additive:

- `SourceDescriptor`: source ID/version, requested/available capability bits, units, cadence and
  privacy/overhead class;
- `CounterEvidence`: cumulative value plus reset/wrap/quality metadata;
- `GaugeEvidence`: instantaneous value plus resolution and uncertainty;
- `IntervalEvidence`: calibrated start/duration and optional thread/context tokens;
- `StackEvidence`: stable thread token, optional Linux TID, fingerprint, trigger and sampled frames;
- `ContextLink`: screen/flow/step/owner/work token to thread/window;
- `QualitySnapshot`: drops, skipped samples, unsupported fields, clock uncertainty and capacity loss;
- `HypothesisEdge`: source and target evidence, temporal relation, positive/missing evidence,
  confidence penalties and alternatives.

Future event IDs and schemas must be append-only at the canonical boundary. The current and any
future ring storage adapters consume the same semantic batch. Unknown future records remain
skippable, and an analyzer without battery support must still read the rest of the log.

## Confidence model

Confidence is evidence-specific, never a single unexplained percentage. Start with a level derived
from the highest inference layer and apply visible penalties for:

- unsupported/permission-denied source or missing baseline;
- clock uncertainty, sampling gaps, queue loss or overwritten/incomplete windows;
- low counter resolution or delta near the calibrated noise floor;
- TID lifetime ambiguity or stack sample far from the CPU/energy window;
- mismatched device/app/network/thermal/screen/charging cohort;
- insufficient repetitions, high variance or order effects;
- simultaneous work from other UIDs, display, modem, GPU or system services;
- app labels that are absent, ambiguous or too high-cardinality;
- competing hypotheses that explain the same delta.

High confidence requires repeatable controlled energy evidence, aligned app/UID activity and
thread/stack evidence, plus materially weaker alternatives. A method stack without controlled
device-energy evidence can be a CPU suspect, never an exact energy allocation.

## Bounded scheduling, overhead and privacy

A future implementation must be off by default and declare a session mode with fixed bounds:

- maximum session duration, active sources, sampled threads and stack definitions;
- minimum sample interval and adaptive backoff for background/thermal/queue pressure;
- fixed queue/cardinality limits and drop-and-count behavior;
- one bounded scheduler/control path rather than one timer/thread per source;
- measured CPU, wakeup, I/O, memory, log-growth and energy overhead against collector-off;
- no raw `/proc`, health-stat or application payload persisted when a typed aggregate suffices;
- allowlisted labels, symbol privacy mode, retention/export controls and explicit QA consent;
- clean stop deadlines and no blocking/I/O on ART callbacks or app hot paths.

An acceptable overhead budget cannot be guessed here. Research must establish separate short
interactive and long battery-session budgets on physical devices. If the collector changes the
energy result by more than the agreed noise fraction, it cannot support the attribution claim.

## V1 extension-point audit

| Required seam | Existing V1 evidence | Readiness / future work |
| --- | --- | --- |
| Extensible source/collector registry | bounded `OptionalIntegrationRegistry`; native `CollectorKind` and `CollectorDescriptor` | usable seam; a future battery module must register descriptors and lifecycle, not add ad-hoc globals |
| Generic evidence primitives | packed semantic event batch with type/schema/flags/time/thread/context/four payload words; existing counter/gauge/interval/stack semantics | ready for additive typed schemas; do not overload fields without a documented schema |
| Stable thread token and Linux TID | bounded native `ThreadRegistry`, lifetime events, optional `linux_tid` | ready; future sampler must handle unsupported TID and reuse races |
| Clock synchronization | monotonic timestamps plus canonical `CLOCK_SYNC` with uncertainty | ready for new sources after source-specific calibration |
| Foreground/screen/flow/owner correlation | foreground flags/lifecycle plus `ContextTracker`, `ContextLink` and ASM propagation | ready; screen on/off is device context and remains distinct from app foreground |
| Storage-neutral semantic events | non-blocking `JankHunterAgentEventSink` and current-v9 adapter | ready; ring implementation still follows the documented seam/checklist |
| Capability descriptors | native capability set, collector descriptor and requested/potential/granted/active status | ready pattern; future source states need typed unsupported/permission reasons |
| Quality/loss/confidence | native/runtime loss snapshots, sequence gaps, bounded CLI aggregation and evidence penalties | ready pattern; calibrator/noise-floor penalties are future work |
| Bounded scheduling extension | bounded integration lifecycle, fixed native containers and trigger budget | partial; define a shared long-session sampler policy before implementation |
| CLI evidence graph beyond UI | canonical stream, general influence/context graph and agent causal findings | partial; add source-agnostic hypothesis nodes/edges rather than a battery-only report island |

The audit is architectural, not a claim that a battery collector already exists. No V1 event or
capability bit enables battery attribution. The `kFutureBatteryEvidence` descriptor value reserves
an extension category only; it schedules nothing and allocates nothing.

## Research questions and acceptance criteria

Before an implementation proposal can move out of backlog, answer with reproducible fixtures and
physical-device evidence:

- Which public signals and individual fields are stable across the supported API/OEM matrix?
- Can Linux TID and per-thread CPU deltas be read with acceptable cost and correct task-lifetime
  handling under thread churn?
- How are foreground/background, display, thermal, network, radio tail, charging and idle state
  normalized or stratified?
- What baseline/candidate protocol, repetitions, randomization and noise threshold are required?
- How are other applications, display/GPU and system-service contributions bounded or disclosed?
- How are unsupported, permission-denied, reset, wrap and below-resolution counters represented?
- Which temporal joins are valid at each clock uncertainty, and which confidence penalties apply?
- What CPU/wakeup/I/O/memory/log/energy overhead is acceptable for a long session?
- Is a separate `BatteryAnalysisMode` necessary, or can a generic evidence-session policy express
  short validation and long sampling without battery-specific core code?
- How do results compare with Perfetto/Power Profiler and, where available, a hardware power monitor?

Minimum research exit criteria:

1. capability matrix and counter-semantics catalog for the declared API/OEM/device set;
2. calibrated idle/CPU/network fixtures and controlled repeated baseline/candidate experiment;
3. source- and end-to-end overhead report including collector-off;
4. schema and bounded-memory/scheduling design review;
5. privacy/threat-model review and export/retention policy;
6. evidence/confidence specification with adversarial confounder tests;
7. validation against at least one independent energy reference;
8. explicit go/no-go decision that preserves `collector disabled = no battery-specific work`.

Until those criteria are met, existing device battery percentage/temperature/context gauges are
context only, ART TI stack/GC evidence is runtime evidence only, and Jank Hunter must not render
method-level battery attribution claims.
