package io.jankhunter.runtime

import android.app.ActivityManager

internal enum class RuntimeUiVisibility(val wireValue: Int) {
    UNKNOWN(0),
    HIDDEN(1),
    VISIBLE(2),
    ;

    companion object {
        fun fromWire(value: Int): RuntimeUiVisibility = when (value) {
            HIDDEN.wireValue -> HIDDEN
            VISIBLE.wireValue -> VISIBLE
            else -> UNKNOWN
        }
    }
}

internal enum class RuntimeProcessImportance(
    val isUserRelevantForSampling: Boolean,
    val wireValue: Long,
) {
    UNKNOWN(false, 0L),
    FOREGROUND(true, 1L),
    FOREGROUND_SERVICE(true, 2L),
    VISIBLE(false, 3L),
    PERCEPTIBLE(false, 4L),
    SERVICE(false, 5L),
    CACHED(false, 6L),
    ;

    companion object {
        fun fromAndroid(value: Int): RuntimeProcessImportance = when {
            value < 0 -> UNKNOWN
            value <= IMPORTANCE_FOREGROUND -> FOREGROUND
            value == IMPORTANCE_FOREGROUND_SERVICE -> FOREGROUND_SERVICE
            value == IMPORTANCE_PERCEPTIBLE_PRE_26 -> PERCEPTIBLE
            value <= IMPORTANCE_VISIBLE -> VISIBLE
            value <= IMPORTANCE_PERCEPTIBLE -> PERCEPTIBLE
            value <= IMPORTANCE_SERVICE -> SERVICE
            else -> CACHED
        }

        private const val IMPORTANCE_FOREGROUND = 100
        private const val IMPORTANCE_FOREGROUND_SERVICE = 125
        private const val IMPORTANCE_PERCEPTIBLE_PRE_26 = 130
        private const val IMPORTANCE_VISIBLE = 200
        private const val IMPORTANCE_PERCEPTIBLE = 230
        private const val IMPORTANCE_SERVICE = 300
    }
}

/** Allocation-free Android process importance port; one instance is owned by the runtime graph. */
internal class AndroidProcessImportanceSource : RuntimeIntSource {
    private val processInfo = ActivityManager.RunningAppProcessInfo()

    override fun getAsInt(): Int {
        ActivityManager.getMyMemoryState(processInfo)
        return processInfo.importance
    }
}
