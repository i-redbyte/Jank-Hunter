package io.jankhunter.runtime.internal.io

import android.os.SystemClock
import io.jankhunter.runtime.JankHunterConfig
import io.jankhunter.runtime.RuntimeLongSource
import java.io.File
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.concurrent.atomic.AtomicReference
import java.lang.ref.WeakReference
import java.io.IOException

/** Creates fully initialized asynchronous sessions; the writer owns only active-session behavior. */
internal class AsyncLogWriterFactory(
    recording: ProcessRecordingSession? = null,
    private val currentTimeMs: RuntimeLongSource = RuntimeLongSource(System::currentTimeMillis),
) {

    private val processRecording = AtomicReference(recording)
    private val writerOwners = ArrayList<WeakReference<AsyncLogWriter>>(2)
    private var creatingWriters = 0

    /** Bound live I/O owners even when a foreign storage callback never returns. */
    private fun createOwnedWriter(recording: ProcessRecordingSession?, create: () -> AsyncLogWriter): AsyncLogWriter {
        if (recording == null) return create()
        synchronized(writerOwners) {
            writerOwners.removeAll { it.get()?.isIoCompleted() != false }
            if (writerOwners.size + creatingWriters >= 2) throw IOException("Jank Hunter writers are still draining")
            creatingWriters++
        }
        var created: AsyncLogWriter? = null
        try { return create().also { created = it } } finally {
            synchronized(writerOwners) {
                try { created?.let { writerOwners.add(WeakReference(it)) } } finally { creatingWriters-- }
            }
        }
    }

    fun resolveDirectory(requested: File): File = processRecording.get()?.resolveDirectory(requested) ?: requested.absoluteFile

    /** The next init must not reuse ownership still being sealed by a retired writer. */
    fun releaseProcessRecording() {
        val previous = processRecording.get() ?: return
        if (processRecording.compareAndSet(previous, ProcessRecordingSession())) previous.retireAfterEpoch()
    }

    fun open(
        directory: File,
        config: JankHunterConfig,
        processName: String,
        expectedProcesses: Set<String> = setOf(processName),
        rosterDeclarationComplete: Boolean = true,
        onTerminalStop: AsyncWriterTerminalObserver = AsyncWriterTerminalObserver.NONE,
        buildIdentity: RuntimeBuildIdentity = RuntimeBuildIdentity.Unknown(RuntimeBuildIdentity.Reason.MISSING),
    ): AsyncLogWriter {
        val recording = processRecording.get()
        val selectedDirectory = if (config.storagePolicy() != null) {
            recording?.resolveDirectory(directory) ?: directory.absoluteFile
        } else {
            directory
        }
        val sessionStartMs = currentTimeMs.getAsLong().coerceAtLeast(0L)
        val localDate = SimpleDateFormat("yyyy-MM-dd", Locale.US).format(Date(sessionStartMs))
        val quality = LogQualityCounters()
        return createOwnedWriter(recording) { AsyncLogWriter(
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
        ) }
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
