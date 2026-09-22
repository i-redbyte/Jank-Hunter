package io.jankhunter.artti

import io.jankhunter.artti.internal.ArtTiNativeBridge

/**
 * Optional hooks for correlating application work with JVMTI stack samples.
 * Safe to call when the ART TI agent is not attached; failures are ignored by callers.
 */
object ArtTiDiagnostics {
    /**
     * Captures a stack sample on the calling thread without consuming the automatic
     * contention/stall sampling budget. Intended for short, explicit evidence points
     * such as a decode on the main thread after a diagnostic scenario.
     */
    fun captureCurrentThreadStack(): Int {
        return ArtTiNativeBridge.nativeCaptureStack(
            thread = Thread.currentThread(),
            trigger = STACK_TRIGGER_EXPLICIT_EVIDENCE,
            contextToken = 0L,
            relatedSequence = 0L,
        )
    }

    private const val STACK_TRIGGER_EXPLICIT_EVIDENCE = 3
}
