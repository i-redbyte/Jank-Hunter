package io.jankhunter.runtime

import android.content.Context
import android.os.SystemClock
import java.io.File

data class JankHunterInitDiagnostics(
    val status: String,
    val failureClass: String? = null,
    val failureMessage: String? = null,
    val processName: String? = null,
    val logDirectory: String? = null,
    val atMs: Long = 0L,
    val attempts: Long = 0L,
    val failures: Long = 0L,
)

/** Stable lifecycle and storage facade. Feature APIs live in their dedicated entry points. */
object JankHunter {
    private val runtime = RuntimeComponentGraph(::nowMs, ::nowUs)

    internal fun instrumentationHooks(): RuntimeInstrumentationHooks = runtime.instrumentationHooks

    internal fun asyncTelemetry(): RuntimeAsyncTelemetry = runtime.asyncTelemetry

    internal fun databaseTracing(): RuntimeManualDatabaseTracing = runtime.manualDatabaseTracing

    internal fun ioTelemetry(): RuntimeIOTelemetry = runtime.ioTelemetry

    internal fun networkTelemetry(): RuntimeNetworkAdapterTelemetry = runtime.networkAdapterTelemetry

    internal fun workerTelemetry(): RuntimeWorkerTelemetry = runtime.workerTelemetry

    internal fun manualTelemetry(): RuntimeManualTelemetry = runtime.manualTelemetry

    @JvmStatic
    fun init(context: Context?) {
        runtime.lifecycle.init(context)
    }

    /**
     * Idempotent, process-local bootstrap used by generated Android component hooks.
     * The one-shot CAS keeps every component invocation after the first one allocation-free.
     */
    @PublishedApi
    @JvmStatic
    internal fun autoInit(context: Context?) {
        runtime.lifecycle.autoInit(context)
    }

    @JvmStatic
    fun init(context: Context?, providedConfig: JankHunterConfig?) {
        runtime.lifecycle.init(context, providedConfig)
    }

    @JvmStatic
    fun isStarted(): Boolean = runtime.lifecycle.isStarted()

    @JvmStatic
    fun isRuntimeEnabled(): Boolean = runtime.lifecycle.isRuntimeEnabled()

    @JvmStatic
    @JvmOverloads
    fun setRuntimeEnabled(enabled: Boolean, reason: String? = "manual"): Boolean {
        return runtime.lifecycle.setRuntimeEnabled(enabled, reason)
    }

    /**
     * Restarts collection with overrides applied to the original build-time configuration.
     * The currently selected binary storage is retained across the restart.
     */
    @JvmStatic
    @JvmOverloads
    fun reconfigure(
        reason: String? = "manual",
        updater: JankHunterConfigUpdater,
    ): Boolean {
        return runtime.lifecycle.reconfigure(reason, updater)
    }

    /**
     * Atomically moves the active session to [storage] without restarting collection. Passing
     * `null` restores Jank Hunter's built-in file storage. The selected storage is retained while
     * runtime collection is disabled and is used by the next runtime start.
     * [JankHunterStorageSwitchResult.IN_PROGRESS] means the ordered switch already started and its
     * final state will be applied asynchronously.
     */
    @JvmStatic
    @JvmOverloads
    fun switchBinaryStorage(
        storage: JankHunterBinaryStorage?,
        timeoutMs: Long = DEFAULT_STORAGE_SWITCH_TIMEOUT_MS,
    ): JankHunterStorageSwitchResult {
        return runtime.storageValve.switchBinaryStorage(storage, timeoutMs)
    }

    @JvmStatic
    fun initDiagnostics(): JankHunterInitDiagnostics = runtime.lifecycle.diagnostics()

    /**
     * Captures immutable file copies without splitting the process recording into additional files.
     * Close the snapshot after consuming its paths to release compatibility copies from the app cache.
     * Returns `null` on the main thread; use [captureLogSnapshotAsync] from UI code.
     */
    @JvmStatic
    fun captureLogSnapshot(): JankHunterLogSnapshot? = runtime.session.captureLogSnapshot()

    /** Captures a snapshot on Jank Hunter's maintenance worker. */
    @JvmStatic
    fun captureLogSnapshotAsync(callback: JankHunterCaptureCallback<JankHunterLogSnapshot>): Boolean {
        return runtime.session.captureLogSnapshotAsync(callback)
    }

    /**
     * Captures every live process at one vector frontier and atomically publishes one ZIP file.
     * Returns `null` on the main thread; use [captureLogArchiveAsync] from UI code.
     */
    @JvmStatic
    fun captureLogArchive(destination: File): JankHunterLogArchive? {
        return runtime.session.captureLogArchive(destination)
    }

    /** Captures and writes an archive on Jank Hunter's maintenance worker. */
    @JvmStatic
    fun captureLogArchiveAsync(
        destination: File,
        callback: JankHunterCaptureCallback<JankHunterLogArchive>,
    ): Boolean {
        return runtime.session.captureLogArchiveAsync(destination, callback)
    }

    /**
     * Exports retained history plus the current coordinated frontier as one `.jhlog.zip` per
     * logical session. Source session directories and archives remain untouched.
     * Returns `null` on the main thread; use [captureSessionArchivesAsync] from UI code.
     */
    @JvmStatic
    fun captureSessionArchives(destinationDirectory: File): List<String>? {
        return runtime.session.captureSessionArchives(destinationDirectory)
    }

    /**
     * Exports session archives with content metadata verified while copying this snapshot.
     * Completed heap dumps remain inside their owning session archive. Call on a worker thread.
     */
    @JvmStatic
    fun captureSessionArchiveArtifacts(destinationDirectory: File): List<JankHunterSessionArchive>? {
        return runtime.session.captureSessionArchiveArtifacts(destinationDirectory)
    }

    /** Exports session logs with optional managed heap dumps for transport size control. */
    @JvmStatic
    fun captureSessionArchives(destinationDirectory: File, includeHeapDumps: Boolean): List<String>? {
        return runtime.session.captureSessionArchives(destinationDirectory, includeHeapDumps)
    }

    /**
     * Includes managed heap dumps only when all exported ZIPs fit [maxBytesIncludingHeapDumps].
     * Oversized payloads skip dumps before copying; ZIP overhead can trigger a logs-only retry.
     * JHLOG files are never truncated or omitted, even if they alone exceed this budget.
     * Source dumps remain unchanged. Hosts must reserve space for their other attachments.
     */
    @JvmStatic
    fun captureSessionArchives(destinationDirectory: File, maxBytesIncludingHeapDumps: Long): List<String>? {
        return runtime.session.captureSessionArchives(destinationDirectory, maxBytesIncludingHeapDumps)
    }

    /** Exports session archives on Jank Hunter's maintenance worker. */
    @JvmStatic
    fun captureSessionArchivesAsync(
        destinationDirectory: File,
        callback: JankHunterCaptureCallback<List<String>>,
    ): Boolean {
        return runtime.session.captureSessionArchivesAsync(destinationDirectory, callback)
    }

    @JvmStatic
    fun flush() {
        runtime.session.flush()
    }

    @JvmStatic
    fun shutdown() {
        runtime.lifecycle.shutdown()
    }

    private fun nowMs(): Long = SystemClock.elapsedRealtime()

    private fun nowUs(): Long = SystemClock.elapsedRealtimeNanos().coerceAtLeast(0L) / 1_000L

    private const val DEFAULT_STORAGE_SWITCH_TIMEOUT_MS = 5_000L
}
