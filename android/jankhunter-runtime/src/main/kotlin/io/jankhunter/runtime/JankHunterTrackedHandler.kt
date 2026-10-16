package io.jankhunter.runtime

import android.os.Handler
import android.os.SystemClock
import java.lang.ref.WeakReference

/**
 * Opt-in Handler boundary. Each returned handle addresses exactly one placement. Unwrapped
 * callbacks use a private native Message token; wrapped callbacks have a unique wrapper identity.
 */
class JankHunterTrackedHandler(private val delegate: Handler) {
    fun post(callback: Runnable): JankHunterPostHandle? = post(callback, 0L, "", 0, 0)

    fun postDelayed(callback: Runnable, delayMillis: Long): JankHunterPostHandle? =
        postDelayed(callback, delayMillis, 0L, "", 0, 0)

    /** Instrumenter entry point; the metadata describes the caller, not this facade. */
    @JvmSynthetic
    fun post(callback: Runnable, siteId: Long, source: String, line: Int, origin: Int): JankHunterPostHandle? =
        submit(callback, siteId, source, line, origin, Long.MIN_VALUE) { actual, token ->
            if (token == null) delegate.post(actual)
            else delegate.postAtTime(actual, token, SystemClock.uptimeMillis())
        }

    /** Instrumenter entry point; the supplied delay is passed to Handler without modification. */
    @JvmSynthetic
    fun postDelayed(callback: Runnable, delayMillis: Long, siteId: Long, source: String,
                    line: Int, origin: Int): JankHunterPostHandle? =
        submit(callback, siteId, source, line, origin, delayMillis) { actual, token ->
            if (token == null) delegate.postDelayed(actual, delayMillis)
            else delegate.postAtTime(actual, token, SystemClock.uptimeMillis() + delayMillis.coerceAtLeast(0L))
        }

    private inline fun submit(
        callback: Runnable, siteId: Long, source: String, line: Int, origin: Int, requestedDelayMillis: Long,
        post: (Runnable, Any?) -> Boolean,
    ): JankHunterPostHandle? {
        val telemetry = JankHunter.concurrencyTelemetry()
        val prepared = RuntimeHookGuard.value<RuntimeCausalHandlerTelemetry.PreparedPost?>(null) {
            telemetry.prepareTrackedHandlerPost(callback, siteId, source, line, origin, requestedDelayMillis)
        }
        val actual = prepared?.callback ?: callback
        val unwrappedHandle = if (actual === callback) {
            JankHunterPostHandle(delegate, actual, prepared?.epochId ?: 0L, prepared?.id ?: 0L, true)
        } else null
        val posted = try {
            post(actual, unwrappedHandle?.removalToken)
        } catch (throwable: Throwable) {
            if (prepared != null) RuntimeHookGuard.run { telemetry.trackedHandlerPostThrew(prepared) }
            throw throwable
        }
        if (prepared != null) RuntimeHookGuard.run { telemetry.trackedHandlerPostReturned(prepared, posted) }
        return if (posted) unwrappedHandle ?:
            JankHunterPostHandle(delegate, actual, prepared?.epochId ?: 0L, prepared?.id ?: 0L)
            else null
    }
}

/** The return value means a removal call was issued; it does not prove that execution stopped. */
class JankHunterPostHandle @JvmOverloads internal constructor(
    handler: Handler, callback: Runnable, private val epochId: Long, private val id: Long,
    private val tokenized: Boolean = false,
) {
    private val handler = WeakReference(handler)
    private val callback = WeakReference(callback)
    internal val removalToken: Any? get() = if (tokenized) callback else null

    fun requestRemoval(): Boolean {
        val targetHandler = handler.get() ?: return false
        val targetCallback = callback.get() ?: return false
        val telemetry = JankHunter.concurrencyTelemetry()
        val token = RuntimeHookGuard.value(0L) { telemetry.handlerRemovalRequested(epochId, id) }
        try {
            if (tokenized) targetHandler.removeCallbacks(targetCallback, callback)
            else targetHandler.removeCallbacks(targetCallback)
        } catch (throwable: Throwable) {
            if (token > 0L) RuntimeHookGuard.run { telemetry.handlerRemovalReturned(epochId, token, true) }
            throw throwable
        }
        if (token > 0L) RuntimeHookGuard.run { telemetry.handlerRemovalReturned(epochId, token, false) }
        return true
    }
}
