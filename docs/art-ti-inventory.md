# ART TI V1 inventory

This inventory records the repository state at commit `f6b25c6` before the ART TI V1 work. It is
an implementation map, not a second architecture specification. The code remains the source of
truth.

## Reusable Android runtime components

| Concern | Existing component | V1 integration decision |
| --- | --- | --- |
| Public lifecycle | `JankHunter`, `RuntimeCoordinator`, `RuntimeState` | Keep `JankHunter` as the facade. A dependency-light internal ART TI service is started and stopped by the existing lifecycle. |
| Early startup | `JankHunterAutoInitProvider` and generated variant manifest | Reuse the provider. The Gradle plugin adds ART TI manifest config and optional artifact packaging only to enabled variants. |
| Process policy | `ProcessNames` and `JankHunterConfig.isProcessAllowed` | Evaluate before attach. No second process policy is introduced. |
| Attribution | `ContextTracker`, `JankHunterContextSnapshot`, ASM work wrappers | Add opaque numeric correlation/context tokens at the native boundary; resolve to the existing screen/flow/step/owner model outside callbacks. |
| High-rate transport | `RuntimeHookEventTransport`, `SpscSlotSequencer` | Reuse its fail-open and cumulative-loss principles. The native callback topology needs a separate MPSC implementation; Kotlin drains batches into the canonical sink. |
| Persistence admission | `AsyncLogWriter` bounded critical/bulk lanes | Add a storage-neutral semantic-event sink in front of the current writer. Agent status and intervals use bounded admission and never block the host app. |
| Quality | `LogQualityCounters`, `QualityCounterId` | Extend cumulative IDs for native queue, interval, decode, capability and shutdown loss. Native counters are saturating and copied in quality snapshots. |
| Current container | `BinaryLogWriter`, `JhlogV9`, length-delimited records | Add append-only agent record types. Unknown future types remain skippable. The native engine does not know v9 record or chunk layout. |
| Storage limits | one bounded v9 session file and cyclic retention | Preserve current behavior. There is no ring-buffer implementation in this revision. |

## Reusable Gradle and packaging components

- `JankHunterExtension` already uses Gradle lazy `Property`/`SetProperty` values.
- `JankHunterPlugin` already owns variant filtering, manifest generation, dependency insertion,
  release-like safety validation and ASM configuration.
- `JankHunterAutomaticDependencies` can add the optional `jankhunter-artti` artifact to only those
  application variants whose effective ART TI mode is not `OFF`.
- Android publishing, Maven metadata and source JAR conventions are centralized in `build-logic`.
- NDK configuration is not currently present. ART TI V1 must pin one NDK version in build logic,
  share warnings/visibility/C++ language settings, and publish `arm64-v8a` plus `x86_64`.

## Reusable CLI components

- `jhlog.StreamFileWithResult` streams committed v9 chunks and distinguishes clean, open-tail and
  corrupt inputs.
- Unknown v9 records already have a length-delimited skip boundary.
- `analyze.collector` is the canonical streaming aggregation entry point.
- `Summary`, `Comparison`, JSON output and the standalone HTML templates are additive extension
  points; logs without agent records must retain their current summaries.
- Influence analysis already has compact graph indexes and explicit runtime/static evidence
  distinctions. ART TI findings should extend this evidence model rather than create an in-process
  causal graph or a separate report product.

## Storage status

No ring-buffer container, decoder or writer exists in this revision. V1 therefore provides:

1. a canonical semantic event boundary between native decoding and storage;
2. the current v9 adapter;
3. an in-memory/fake sink used for semantic equivalence and loss tests; and
4. an explicit ring-buffer integration checklist in the architecture ADR.

It deliberately does not invent a second on-disk ring format.

## Platform compatibility facts

| Platform fact | V1 consequence |
| --- | --- |
| ART TI runtime support starts in Android 8.0 / API 26. | The SDK can describe ART TI as unavailable below API 26. |
| Public in-process `Debug.attachJvmtiAgent` was added in API 28. | Automatic SDK-owned runtime attachment is API 28+. |
| Android and ART both require a debuggable app for attachment. | The SDK reads `ApplicationInfo.FLAG_DEBUGGABLE`; it never attempts to mutate debuggability. |
| ART exposes a partial JVMTI implementation and capabilities vary by release/device. | Every requested capability is intersected with `GetPotentialCapabilities`, verified with `GetCapabilities`, and reported. |
| AOSP currently implements thread CPU JVMTI calls as `JVMTI_ERROR_NOT_IMPLEMENTED`. | Thread CPU attribution is not part of V1 and is not used as a design foundation. |
| GC callbacks run while the VM is stopped and severely restrict JNI/JVMTI calls. | The callbacks only read the monotonic clock and publish a fixed native event. |

Primary references:

- [AOSP ART TI overview and attach restrictions](https://source.android.com/docs/core/runtime/art-ti)
- [Android 28 `Debug.attachJvmtiAgent` API addition](https://developer.android.com/sdk/api_diff/28/changes/android.os.Debug)
- [AOSP ART JVMTI capability negotiation](https://android.googlesource.com/platform/art/+/master/openjdkjvmti/OpenjdkJvmTi.cc)
- [JVMTI specification](https://docs.oracle.com/en/java/javase/24/docs/specs/jvmti.html)

The AOSP `master` capability bitmap is useful evidence, but it is not treated as a device matrix.
The negotiated result from the running ART is authoritative.
