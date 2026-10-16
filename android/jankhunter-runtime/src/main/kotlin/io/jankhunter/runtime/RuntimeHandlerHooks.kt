package io.jankhunter.runtime

import android.os.Handler
import io.jankhunter.runtime.internal.io.QualityCounterId

/** Legacy invocation signatures accept hints without inventing callback identity. */
internal interface LegacyHandlerCallbackContract {
    fun onPostResult(original: Runnable, wrapped: Runnable, posted: Boolean)
    fun wrap(handler: Handler, runnable: Runnable, token: Any?, ownerName: String?): Runnable
    fun wrappers(handler: Handler, runnable: Runnable, token: Any?): Array<Runnable>
    fun clear(handler: Handler, runnable: Runnable, token: Any?)
    fun clear(handler: Handler, token: Any?)
    fun clear()
}

/** Legacy hook ABI adapter. Android must always see the application's original callback identity. */
internal class RuntimeHandlerHooks(private val access: RuntimeTelemetryAccess) : LegacyHandlerCallbackContract {
    override fun onPostResult(original: Runnable, wrapped: Runnable, posted: Boolean) {
        if (posted && access.isFeatureActive(JankHunterRuntimeFeature.HANDLERS)) {
            // Successful submission does not prove execution: uninstrumented code may cancel it.
            // Without a platform dispatch token, neither identity nor current context identifies this post.
            access.writer?.recordQuality(QualityCounterId.HANDLER_POST_CONTEXT_UNAVAILABLE)
        }
    }

    override fun wrap(handler: Handler, runnable: Runnable, token: Any?, ownerName: String?): Runnable = runnable

    override fun wrappers(handler: Handler, runnable: Runnable, token: Any?): Array<Runnable> = noWrappers

    override fun clear(handler: Handler, runnable: Runnable, token: Any?) = Unit

    override fun clear(handler: Handler, token: Any?) = Unit

    override fun clear() = Unit

    private companion object {
        val noWrappers = emptyArray<Runnable>()
    }
}
