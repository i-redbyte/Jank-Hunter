package io.jankhunter.runtime.internal.io

import android.os.SystemClock
import io.jankhunter.runtime.JankHunterConfig
import io.jankhunter.runtime.RuntimeLongSource
import java.io.File
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

/** Creates fully initialized asynchronous sessions; the writer owns only active-session behavior. */
internal class AsyncLogWriterFactory(
    private val recording: ProcessRecordingSession? = null,
    private val currentTimeMs: RuntimeLongSource = RuntimeLongSource(System::currentTimeMillis),
) {

    fun resolveDirectory(requested: File): File = recording?.resolveDirectory(requested) ?: requested.absoluteFile

    fun open(
        directory: File,
        config: JankHunterConfig,
        processName: String,
        expectedProcesses: Set<String> = setOf(processName),
        rosterDeclarationComplete: Boolean = true,
        onTerminalStop: AsyncWriterTerminalObserver = AsyncWriterTerminalObserver.NONE,
        buildIdentity: RuntimeBuildIdentity = RuntimeBuildIdentity.Unknown(RuntimeBuildIdentity.Reason.MISSING),
    ): AsyncLogWriter {
        val selectedDirectory = if (config.storagePolicy() != null) {
            resolveDirectory(directory)
        } else {
            directory
        }
        val sessionStartMs = currentTimeMs.getAsLong().coerceAtLeast(0L)
        val localDate = SimpleDateFormat("yyyy-MM-dd", Locale.US).format(Date(sessionStartMs))
        val quality = LogQualityCounters()
        return AsyncLogWriter(
            directory = selectedDirectory,
            config = config,
            processName = processName,
            expectedProcesses = expectedProcesses,
            rosterDeclarationComplete = rosterDeclarationComplete,
            sessionStartMs = sessionStartMs,
            sessionLocalDate = localDate,
            collectorStartElapsedUs = nowElapsedUs(),
            currentTimeMs = currentTimeMs,
            quality = quality,
            prepareSession = {
                prepareSession(selectedDirectory, config, processName, expectedProcesses, rosterDeclarationComplete)
            },
            onTerminalStop = onTerminalStop,
            buildIdentity = buildIdentity,
            recording = recording,
        )
    }

    private fun prepareSession(
        directory: File,
        config: JankHunterConfig,
        processName: String,
        expectedProcesses: Set<String>,
        rosterDeclarationComplete: Boolean,
    ): LogGrowthManager? {
        if (config.deleteObsoleteJhlogFormats()) {
            try {
                ObsoleteSessionLogCleaner.clean(directory, config.binaryStorage())
            } catch (error: Throwable) {
                if (error.isFatal()) throw error
            }
        }
        return createLogGrowthManager(directory, config, processName, expectedProcesses, rosterDeclarationComplete)
    }

    private fun createLogGrowthManager(
        directory: File,
        config: JankHunterConfig,
        processName: String,
        expectedProcesses: Set<String>,
        rosterDeclarationComplete: Boolean,
    ): LogGrowthManager? {
        if (!config.logGrowthAnalyticsEnabled()) return null
        return try {
            if (rosterDeclarationComplete && processName in expectedProcesses) {
                LogGrowthHistoryStore.deleteObsoleteProcessScopes(directory, expectedProcesses)
            }
            LogGrowthManager(directory, processName, currentTimeMs)
        } catch (error: Throwable) {
            if (error.isFatal()) throw error
            null
        }
    }

    private fun nowElapsedUs(): Long = SystemClock.elapsedRealtimeNanos().coerceAtLeast(0L) / 1_000L

    private fun Throwable.isFatal(): Boolean = this is VirtualMachineError || this is ThreadDeath
}
