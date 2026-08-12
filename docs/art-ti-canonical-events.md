# ART TI canonical events and storage seam

Status: implemented V1 contract. The C++ batch protocol, this semantic model, the v9 container and
the CLI input stream are deliberately separate version boundaries.

## Runtime boundary

`JankHunterAgentEventSink` is the storage-neutral, non-blocking boundary. Native drains are decoded
into a reusable `JankHunterAgentEventBatch`: one packed `LongArray`, fixed capacity, no JVM object
per event. A sink must consume or copy a batch before `tryPublish` returns. The current v9 adapter
makes one owned packed-array copy per accepted batch because the asynchronous writer may outlive the
next drain. Rejection is fail-open and increments both ART TI and writer quality evidence.

Low-rate definitions use two explicit methods on the same sink:

- `tryPublishContext` defines a `contextToken` with bounded screen/owner/flow/step attribution;
- `tryPublishMethodDefinition` defines a process-local JVMTI method ID with a bounded code symbol.

Neither method carries application object contents, arguments, return values or `toString()` data.

## Common event fields

Every event has semantic type and schema version plus these bounded fields:

| Field | Meaning / ordering | Identity lifetime |
| --- | --- | --- |
| `monotonicNs` | native monotonic observation time; v9 stores microsecond precision in the envelope | process boot clock |
| `producerSequence` | native admission sequence; gaps mean loss, zero is an SDK-generated definition/status | process instance |
| `producerId` | reserved bounded native source ID | process instance |
| `threadToken` | opaque non-zero thread identity; never a `jthread`/`jobject` | process instance |
| `contextToken` | opaque FNV-derived correlation key | process instance |
| `flags` | semantic-type-specific bitset | event |
| `payload0..3` | fixed-width schema-specific values | event |

The v9 adapter writes one length-delimited `AGENT_EVENT` (record type 17) per semantic event. Its
payload prefix is eleven unsigned varints in this order: semantic type, schema version, producer
sequence, producer ID, thread token, context token, flags and payload 0–3. Known schema readers
ignore trailing fields. Unsupported semantic schemas and unknown record types are skipped without
invalidating the committed chunk.

## V1 semantic schemas

All rows below use semantic schema 1. Empty fields are zero.

| Type | `payload0` | `payload1` | `payload2` | `payload3` | Merge / cardinality |
| --- | --- | --- | --- | --- | --- |
| `AgentStatus` | status/reason | detail or effective profile | config hash | native memory bytes | latest plus ordered lifecycle |
| `AgentCapability` | requested bits | potential bits | granted bits | active bits | latest matrix |
| `AgentQualitySnapshot` | queue high-watermark | queue-full total | admission-contention total | other native loss total | latest cumulative maximum |
| `ThreadStart/End` | Linux TID | hashed name ID | packed state/category | daemon/update fields | bounded by configured thread table |
| `GcInterval` | start ns | duration ns | related token | reason | sum/count/top-k; already paired |
| `MonitorContentionInterval` | start ns | duration ns | related token | reason | sum/count/top-k; already paired |
| `ThreadStackSample` | stack fingerprint | related sequence | packed frame count/trigger | truncation | bounded samples and top-k fingerprints |
| `StackDefinition` | fingerprint | process-local method ID | bytecode/native location | frame index + total | bounded fingerprint table |
| `ClockSync` | SDK elapsed midpoint ns | sampling uncertainty ns | reserved | reserved | nearest calibration point |
| `CorrelationLink` | context token | reserved | reserved | reserved | latest thread-token/context mapping |
| `MethodDefinition` | process-local method ID | reserved | reserved | reserved | optional trailing local method symbol ref |

`CorrelationLink` with `CONTEXT_DEFINITION` set defines token-to-attribution using the v9 record
envelope. Without it, the native event links `threadToken` to `contextToken`. Definitions are
bounded; overflow or persistence rejection is quality evidence, never an unbounded retry.

## Privacy and interpretation

- Status/capability/quality/clock fields are operational metadata.
- Thread and method IDs are process-local. Method strings are code signatures only and use the
  bounded v9 dictionary; they are not stable identifiers across processes or builds.
- Context strings follow existing Jank Hunter screen/owner/flow/step policy.
- GC and contention are measured intervals. Overlap with a stall is direct temporal evidence, not
  proof that the interval caused the stall.
- Sequence gaps, missing definitions, incomplete storage windows and capability mismatches reduce
  confidence. Paired starts without finishes never become completed intervals.

## Future ring-buffer adapter checklist

No repository ring-container contract exists yet, so V1 does not invent a competing on-disk format.
The adapter merged with that work must:

1. implement `JankHunterAgentEventSink` and preserve session/process/generation, monotonic time,
   producer sequence and thread/context tokens;
2. copy or commit an accepted batch atomically before returning and expose rejection/overwrite loss;
3. make each record/block independently length-delimited, checksummed and committed;
4. retain or periodically re-emit latest status, capability, quality, clock, context, thread,
   method and stack definitions after wrap;
5. expose generation/epoch and sequence gaps to the canonical CLI stream;
6. tolerate a missing session beginning, torn tail, overwritten pair member and no clean close;
7. never synthesize a completed GC/contention interval from unmatched endpoints;
8. pass semantic-equivalence goldens against the v9 adapter when no overwrite occurs, and emit
   incomplete-window/data-gap quality with lower confidence when overwrite is injected.

The in-memory sink used by runtime tests consumes the same packed batch contract and copies before
reuse; it is the executable adapter seam until the shared ring storage lands.
