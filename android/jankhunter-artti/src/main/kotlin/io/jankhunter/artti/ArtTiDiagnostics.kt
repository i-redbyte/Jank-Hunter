package io.jankhunter.artti

import io.jankhunter.artti.internal.ArtTiContextTokens
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
        val thread = Thread.currentThread()
        val contextToken = currentContextToken()
        if (contextToken != 0L) {
            ArtTiNativeBridge.nativeLinkThreadContext(thread, contextToken)
        }
        return ArtTiNativeBridge.nativeCaptureStack(
            thread = thread,
            trigger = STACK_TRIGGER_EXPLICIT_EVIDENCE,
            contextToken = contextToken,
            relatedSequence = 0L,
        )
    }

    private fun currentContextToken(): Long {
        val snapshot = runCatching {
            val telemetry = Class.forName("io.jankhunter.runtime.JankHunterTelemetry")
            telemetry.getMethod("contextSnapshot").invoke(null)
        }.getOrNull() ?: return 0L
        val screen = snapshot.javaClass.getMethod("getScreen").invoke(snapshot) as? String
        val owner = snapshot.javaClass.getMethod("getOwner").invoke(snapshot) as? String
        val flow = snapshot.javaClass.getMethod("getInitiatorName").invoke(snapshot) as? String
        val operationId = (snapshot.javaClass.getMethod("getOperationId").invoke(snapshot) as? Long) ?: 0L
        val step = if (operationId != 0L) operationId.toString() else null
        return ArtTiContextTokens.token(screen, owner, flow, step)
    }

    private const val STACK_TRIGGER_EXPLICIT_EVIDENCE = 3
}
