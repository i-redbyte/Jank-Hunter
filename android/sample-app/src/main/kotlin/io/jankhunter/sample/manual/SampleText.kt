package io.jankhunter.sample.manual

import android.content.Context

internal class SampleText(private val context: Context) {
    operator fun invoke(resourceId: Int, vararg formatArgs: Any): String {
        return context.getString(resourceId, *formatArgs)
    }
}
