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
     * Seals a coordinated vector frontier and immediately continues collection in new segments.
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
     * Fail-open diagnostic counter for optional runtime integrations (for example ART TI).
     * No-ops when collection is inactive or the name is blank.
     */
    @JvmStatic
    fun recordIntegrationCounter(name: String?, value: Long) {
        if (name.isNullOrBlank() || value == 0L) return
        try {
            runtime.recordIntegrationCounter(name, value)
        } catch (_: Throwable) {
            // Optional integrations must never disturb application threads.
        }
    }

    /**
     * Fail-open diagnostic gauge for optional runtime integrations (for example ART TI).
     * No-ops when collection is inactive or the name is blank.
     */
    @JvmStatic
    fun recordIntegrationGauge(name: String?, value: Long) {
        if (name.isNullOrBlank()) return
        try {
            runtime.recordIntegrationGauge(name, value)
        } catch (_: Throwable) {
            // Optional integrations must never disturb application threads.
        }
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
