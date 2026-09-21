package io.jankhunter.runtime

import android.os.Handler
import io.jankhunter.runtime.internal.io.QualityCounterId

/** Legacy hook ABI adapter. Android must always see the application's original callback identity. */
internal class RuntimeHandlerHooks(private val access: RuntimeTelemetryAccess) {
    fun onPostResult(original: Runnable, wrapped: Runnable, posted: Boolean) {
        if (posted && access.isFeatureActive(JankHunterRuntimeFeature.HANDLERS)) {
            // Successful submission does not prove execution: uninstrumented code may cancel it.
            // Without a platform dispatch token, neither identity nor current context identifies this post.
            access.writer?.recordQuality(QualityCounterId.HANDLER_POST_CONTEXT_UNAVAILABLE)
        }
    }

    fun wrap(handler: Handler, runnable: Runnable, token: Any?, ownerName: String?): Runnable = runnable

    fun wrappers(handler: Handler, runnable: Runnable, token: Any?): Array<Runnable> = noWrappers

    fun clear(handler: Handler, runnable: Runnable, token: Any?) = Unit

    fun clear(handler: Handler, token: Any?) = Unit

    fun clear() = Unit

    private companion object {
        val noWrappers = emptyArray<Runnable>()
    }
}
