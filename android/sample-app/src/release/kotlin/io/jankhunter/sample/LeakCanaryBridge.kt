package io.jankhunter.sample

import android.content.Context

internal object LeakCanaryBridge {
    fun configure() = Unit

    @Suppress("UNUSED_PARAMETER")
    fun watch(watchedObject: Any, description: String) = Unit

    fun status(context: Context): String = context.getString(R.string.leakcanary_release_status)
}
