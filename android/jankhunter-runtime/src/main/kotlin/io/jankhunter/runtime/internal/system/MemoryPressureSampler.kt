package io.jankhunter.runtime.internal.system

import android.app.ActivityManager
import io.jankhunter.runtime.RuntimeIntSource

internal class MemoryPressureSampler(
    private val recordGauge: NamedLongRecorder,
    private val readTrimLevel: RuntimeIntSource = RuntimeIntSource(ProcessTrimLevelReader()::read),
) {
    fun sample() {
        val level = try {
            readTrimLevel.getAsInt()
        } catch (_: Exception) {
            return
        }
        if (level >= 0) {
            recordGauge.record(TRIM_LEVEL_METRIC, level.toLong())
        }
    }

    private companion object {
        const val TRIM_LEVEL_METRIC = "memory.trim.last_level"
    }
}

private class ProcessTrimLevelReader {
    private val processInfo = ActivityManager.RunningAppProcessInfo()

    fun read(): Int {
        ActivityManager.getMyMemoryState(processInfo)
        return processInfo.lastTrimLevel
    }
}
