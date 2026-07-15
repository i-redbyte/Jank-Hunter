package io.jankhunter.runtime.internal.system

/**
 * Strength of the runtime observation behind a retained-object event.
 *
 * A runtime observation never claims a leak: only the CLI can promote it to a confirmed HPROF
 * path after following references from a recognized GC root.
 */
internal enum class RetentionEvidence(val wireValue: Long) {
    TIME_ONLY(1L),
    AFTER_EXPLICIT_GC(2L),
}
