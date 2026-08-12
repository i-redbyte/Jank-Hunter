# ADR 0001: ART TI agent V1 boundaries

- Status: accepted for implementation
- Date: 2026-08-12
- Scope: first production-quality ART TI agent based on JVMTI

## Decision drivers

The priority order is host application safety, data correctness, predictable overhead and metric
completeness. ART callbacks execute in a process owned by the host application, sometimes while
the VM is stopped. Every callback path must therefore have bounded time and memory and must be able
to lose diagnostics without delaying or changing the host application.

V1 collects negotiated lifecycle, GC, Java-thread, monitor-contention and triggered stack evidence.
It does not collect battery signals, allocations, method entry/exit, object contents or continuous
stack samples.

## Component and data flow

```text
ART callbacks
  -> thin JVMTI adapter
  -> fixed NativeEvent
  -> preallocated bounded MPSC transport
  -> native batch encoder
  -> versioned JNI direct-buffer drain
  -> Kotlin protocol decoder
  -> canonical AgentEvent batches
  -> AgentEventSink
       -> current .jhlog v9 adapter
       -> in-memory semantic test sink
       -> future ring-buffer adapter
  -> CLI InputDecoder
  -> canonical CLI event stream
  -> bounded temporal indexes / evidence findings / JSON / HTML
```

The control plane is separate: Gradle resolves an immutable effective config, the SDK checks
eligibility, public Android attachment loads the agent, a JNI handshake checks protocol/config,
the adapter negotiates exact capabilities, and stop disables event notification before a bounded
drain and resource release.

## Physical modules

- `jankhunter-artti`: optional Android AAR containing Kotlin integration and the native shared
  library. It depends on `jankhunter-runtime`, but the runtime never depends on it. The SDK/Gradle
  plugin discovers the integration through a small dependency-light runtime entry point.
- `jankhunter-artti/src/main/cpp/core`: platform-neutral C++20 event, transport, interval, clock,
  quality and protocol code with host tests and benchmarks.
- `jankhunter-artti/src/main/cpp/art`: the only production code that owns `jvmtiEnv`, `JNIEnv`,
  `jthread`, `jobject` or `jmethodID` semantics.
- Existing runtime and CLI modules receive only canonical model, storage adapter, analysis and
  report extensions.

The native core stays a private CMake target; it is not a public Maven artifact.

## Callback and transport contract

`NativeEvent` is a fixed-layout value. It contains protocol/schema type, monotonic timestamp,
producer sequence, process-local thread/context IDs, flags and a bounded typed payload. Callback
publication performs no allocation, I/O, JNI upcall, symbol lookup, string formatting, sleep,
condition wait or blocking lock.

The transport is a preallocated bounded MPSC ring with one Kotlin/JNI drain consumer. Each slot has
a monotonically increasing sequence and a fixed event payload. Producers reserve a position using
a bounded number of compare-and-set attempts, write the payload and release-publish the slot.
The consumer acquire-loads the sequence, copies the event and release-marks the slot reusable.

Invariants:

1. A slot is owned by at most one producer or the consumer at a time.
2. A published sequence makes the complete preceding payload visible to the consumer.
3. A full or contended queue causes a drop and saturating counter increment; it never waits.
4. Publication is linearized by the successful producer-position CAS.
5. Drain is linearized when the consumer advances its position after copying a published slot.
6. Stop first closes admission, then disables JVMTI notifications, waits for active callbacks to
   leave, performs a deadline-bounded drain and only then frees the engine.
7. Sequence arithmetic uses unsigned differences and rejects capacities that cannot preserve the
   half-range ordering invariant.

A single MPSC ring was chosen over per-producer SPSC rings because ART invokes callbacks on a
dynamic set of application/runtime threads. MPSC removes callback-time producer allocation,
registry exhaustion and dead-thread reclamation from the correctness-critical path. The bounded
CAS budget makes contention loss explicit.

## Interval correlation

- GC uses one atomic open interval. The JVMTI specification promises paired start/finish events and
  no intervening GC event when both notifications are enabled. Duplicate/orphan boundaries update
  quality counters and do not create a false completed interval.
- Monitor contention uses a bounded open-addressed table keyed by `threadToken`. Enter claims an
  empty/key slot with bounded probing; entered removes the matching start and publishes one
  `MonitorContentionInterval` only when it exceeds the configured duration.
- Thread tokens are monotonically assigned, process-local and never derived from `jthread` values.
  `jthread`/`jobject` handles never enter the event protocol or storage.
- Thread-local JVMTI storage links callbacks to tokens. Metadata enrichment uses `GetAllThreads`
  and `GetThreadInfo` only on the control/drain path and releases all JVMTI-allocated memory.
- Incomplete capacity/shutdown/orphan pairs become cumulative quality evidence, never fake
  zero-duration intervals.

## Triggered stack policy

Stacks are never continuously collected in `LIGHT` or `CAUSAL`. Kotlin requests a bounded capture
for the main thread near an existing stall/jank trigger or for a long-contention token, subject to
minimum interval and per-minute budgets. `GetStackTrace` runs outside JVMTI callbacks into a fixed
maximum frame buffer. Callback events contain only runtime method IDs. Method/class names are
resolved asynchronously into bounded definitions and are not read from arguments, locals, object
fields or return values.

The native boundary also enforces a defensive preset budget even if a Kotlin caller is faulty:
`CAUSAL`/`CUSTOM` allow at most 120 captures per minute with a 250 ms minimum interval, `DEEP`
allows 600 with a 50 ms minimum interval, and `OFF`/`LIGHT` reject captures. The effective SDK
configuration may impose a stricter budget before crossing JNI.

V1 uses a measured 64-bit stack fingerprint and a bounded open-addressed definition table. A full
table emits the sample with an unresolved fingerprint and a quality loss counter; it never grows.
The same fixed-capacity table bounds process-local method IDs. Lookup is a maximum of 32 probes;
pathological collisions are counted as capacity loss instead of making control-path latency
unbounded.

## Native protocol boundary

- exported ABI version and protocol version;
- C structs start with `structSize` and contain fixed-width integers only;
- feature/capability bit masks;
- explicit status codes and required-buffer-size results;
- every record is length-delimited and unknown record types are skippable;
- all external lengths, capacities and offsets are checked before use;
- no STL type, exception, RTTI object or ownership crosses JNI/C ABI;
- hidden visibility by default; only `Agent_OnAttach`, `Agent_OnLoad` and required JNI functions are
  exported;
- Kotlin drains into a reusable direct `ByteBuffer`; no Java/Kotlin object is created per native
  event in the native/JNI layer.

## Lifecycle

```text
UNINITIALIZED
  -> INELIGIBLE
  -> ATTACHING
  -> ACTIVE | DEGRADED | FAILED
  -> STOPPING
  -> STOPPED
```

State transitions use compare-and-set and are idempotent. Partial capability failure produces
`DEGRADED`. Any internal diagnostic failure closes native admission and disables events. Attachment
failure is recorded but never escapes application startup. A second initialize/attach does not
create another engine or consumer. Stop has a deadline and reports clean, timeout or fail-open.

## Capability policy

Presets request only what they use:

| Profile | Requested signals |
| --- | --- |
| `OFF` | no artifact where variant policy can exclude it; never attach |
| `LIGHT` | agent status, thread identity and GC |
| `CAUSAL` | `LIGHT`, monitor contention and triggered stacks |
| `DEEP` | larger bounded budgets for the same V1 collectors |
| `CUSTOM` | explicit subset within build-time bounds |

`GetPotentialCapabilities`, `AddCapabilities` and `GetCapabilities` results are persisted
separately as requested/potential/granted/active masks. V1 requests
`can_generate_garbage_collection_events` and `can_generate_monitor_events` only when enabled.
Thread lifecycle and stack operations are enabled only after their actual JVMTI calls succeed.
Method entry/exit and VM object allocation remain disabled for every V1 preset.

## Canonical events and storage

The canonical model contains `AgentStatus`, `AgentCapability`, `AgentQualitySnapshot`,
`ThreadLifecycle`, `GcInterval`, `MonitorContentionInterval`, `ThreadStackSample`,
`StackDefinition`, `ClockSync` and `CorrelationLink`. Events carry schema version, monotonic time,
process/session/producer identity and producer sequence. String-heavy values are definitions;
runtime events reference bounded numeric IDs.

`AgentEventSink.tryPublish(batch)` is non-blocking and storage-neutral. The current adapter maps
canonical events to additive v9 record types. A future ring adapter must preserve canonical
semantics, not native wire bytes.

Ring-buffer integration checklist:

1. map committed records/blocks to the canonical event stream;
2. preserve session/process/producer/sequence/generation identities;
3. surface overwrite, torn region, gap and missing-checkpoint quality;
4. periodically checkpoint status/capabilities/config/thread/stack definitions;
5. allow reading without the beginning or clean end of a session;
6. prove semantic equivalence against the in-memory/current adapter fixture;
7. reduce finding confidence for incomplete windows without failing analysis.

## Ordering and evidence

Native monotonic time is calibrated against `SystemClock.elapsedRealtimeNanos()` with periodic
`ClockSync` points. CLI temporal joins use a bounded reorder window and deterministic ordering by
`(monotonicTime, producerId, producerSequence, eventType)`. Direct measured intervals stay facts.
Overlap and multiple corroborating signals produce associations; temporal coincidence alone is
labelled correlation or hypothesis. Drops, gaps, capability/config mismatch and incomplete windows
penalize confidence.

## Failure modes

| Failure | Behavior | Evidence |
| --- | --- | --- |
| API below 28 | no attach | `AgentStatus(API_UNSUPPORTED)` when storage is available |
| App not debuggable | no attach | one rate-limited diagnostic and `APP_NOT_DEBUGGABLE` |
| ABI/library missing | no attach | `LIBRARY_MISSING`/`ABI_UNSUPPORTED` |
| Attach exception/security rejection | host startup continues | bounded failure class/reason |
| Capability unavailable | collector not enabled | requested/potential/granted/active matrix, `DEGRADED` |
| Native queue full/CAS contention | drop | saturating counters, high-watermark |
| Interval table full/orphan | no completed interval | cumulative incomplete/orphan counters |
| Kotlin direct buffer invalid/corrupt batch | reject whole invalid record/batch | decoder counter; disable after bounded repeated failures |
| Current storage rejects batch | drop after decode | sink rejection counter |
| Stop deadline expires | no blocking beyond deadline | `SHUTDOWN_TIMEOUT`; engine remains safely gated/leaked rather than freed under callback |
| Internal native fatal diagnostic state | disable event notifications/admission | `FAILED_OPEN`; host app continues unless process integrity is already lost |

The agent does not attempt signal-handler recovery from memory corruption or native crashes.

## Initial budgets and release gates

Defaults are configuration bounds, not performance claims:

- native transport: 4,096 events, fixed after initialization;
- native event: at most 128 bytes, queue payload at most 512 KiB;
- open contention intervals: 1,024;
- tracked threads: 512;
- stack depth: 64 (`DEEP` maximum 128);
- stack definitions: 1,024; method definitions: 4,096;
- drain batch: 256 records and at most 256 KiB;
- callback publish: O(1), zero post-init allocation, no blocking, target p99 below 10 us on a
  representative Android arm64 device without stack walking;
- shutdown: 500 ms default total deadline;
- native memory budget and actual high-watermark are emitted in status.

Host benchmarks are required for algorithm regression. Final device gates require baseline,
artifact-present/OFF, CAUSAL and overload runs on declared hardware and record startup, frame time,
CPU, RSS/PSS/native heap, binary/APK size, latency percentiles, throughput and drops. Unmeasured
device cells remain `not measured`; they are never represented as zero.

## Rejected alternatives

- Java callback per JVMTI event: rejected because it allocates/crosses JNI on the callback path and
  amplifies host pauses.
- Mutex-protected `std::queue`: rejected because callback progress would depend on another thread.
- Unbounded vector/map/dictionary: rejected because diagnostic load could exhaust host memory.
- Per-producer SPSC rings: rejected for V1 because dynamic ART thread registration and reclamation
  add callback-time capacity and lifetime hazards; it may be reconsidered only with measurements.
- Encoding `.jhlog` in C++: rejected because it couples the engine to a storage container and would
  duplicate current/ring adapters.
- Persisting raw start/finish pairs: rejected because ring wrap/drop could manufacture misleading
  completed intervals.
- Continuous all-thread stack sampling: rejected due to unpredictable runtime and memory cost.
- `MethodEntry`, `MethodExit`, `VMObjectAlloc`: rejected for all V1 profiles.
- JVMTI thread CPU time: rejected because AOSP currently returns `NOT_IMPLEMENTED`.
- Hidden APIs or `BinderInternal.addGcWatcher`: rejected as unsupported and unsafe.
- A new ring-buffer file format: rejected because no current repository contract exists to extend.
- Battery collection in V1: rejected as out of scope; the architecture exposes future collector and
  evidence seams with zero battery-specific work today.
