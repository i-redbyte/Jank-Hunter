package io.jankhunter.runtime

import android.os.Handler
import io.jankhunter.runtime.internal.io.QualityCounterId

internal class RuntimeHandlerHooks(
    private val access: RuntimeTelemetryAccess,
    private val callbacks: RuntimeAsyncCallbacks,
) : HandlerRunnableOwner {
    private val wrappers = HandlerWrapperRegistry(
        droppedCounter = { loss ->
            val qualityId = when (loss) {
                HandlerWrapperLoss.ENTRY_LIMIT -> QualityCounterId.HANDLER_ENTRY_LIMIT
                HandlerWrapperLoss.WRAPPER_LIMIT -> QualityCounterId.HANDLER_WRAPPER_LIMIT
                HandlerWrapperLoss.CONTENTION -> QualityCounterId.HANDLER_CONTENTION_BYPASS
            }
            access.writer?.recordQuality(qualityId)
        },
        exactAdmission = { access.config?.exactEventCollectionEnabled() != false },
    )

    override fun unregister(delegate: Runnable, wrapper: Runnable) {
        RuntimeHookGuard.run { wrappers.unregister(delegate, wrapper) }
    }

    fun onPostResult(original: Runnable, wrapped: Runnable, posted: Boolean) {
        if (!posted && wrapped !== original) {
            wrappers.unregister(original, wrapped)
        }
    }

    fun wrappers(handler: Handler, runnable: Runnable, token: Any?): Array<Runnable> {
        return wrappers.wrappers(handler, runnable, token)
    }

    fun clear(handler: Handler, runnable: Runnable, token: Any?) {
        wrappers.unregister(handler, runnable, token)
    }

    fun clear(handler: Handler, token: Any?) {
        wrappers.unregister(handler, token)
    }

    fun wrap(handler: Handler, runnable: Runnable, token: Any?, ownerName: String?): Runnable {
        val wrapper = wrapHandlerRunnableDecorator(
            runnable,
            ownerName,
            access.isActive(),
            callbacks,
            this,
        )
        if (wrapper === runnable) return runnable
        val config = access.config
        val maxEntries = config?.maxHandlerTrackingEntries() ?: DEFAULT_MAX_TRACKING_ENTRIES
        val maxWrappers = config?.maxHandlerWrappersPerRunnable() ?: DEFAULT_MAX_WRAPPERS_PER_RUNNABLE
        if (!wrappers.register(handler, runnable, token, wrapper, maxEntries, maxWrappers)) {
            return runnable
        }
        return wrapper
    }

    fun clear() {
        wrappers.clear()
    }

    private companion object {
        const val DEFAULT_MAX_TRACKING_ENTRIES = 4096
        const val DEFAULT_MAX_WRAPPERS_PER_RUNNABLE = 32
    }
}
