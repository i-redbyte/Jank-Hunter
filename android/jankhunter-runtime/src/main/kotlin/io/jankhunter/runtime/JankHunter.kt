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
    private class RuntimeReference {
        object Holder {
            @JvmField val value = RuntimeComponentGraph(::nowMs, ::nowUs)
        }
        @Volatile private var readyHooks: RuntimeInstrumentationHooks? = null

        fun initialize(): RuntimeComponentGraph = Holder.value.also { readyHooks = it.instrumentationHooks }
        fun hooksIfReady(): RuntimeInstrumentationHooks? = readyHooks
    }
    private val runtime = RuntimeReference()
    private val graph: RuntimeComponentGraph get() = RuntimeReference.Holder.value

    private fun initializedRuntime(): RuntimeComponentGraph = runtime.initialize()

    internal fun workerBuilderHooksOrNull(): RuntimeInstrumentationHooks? = runtime.hooksIfReady()

    internal fun instrumentationHooks(): RuntimeInstrumentationHooks = RuntimeReference.Holder.value.instrumentationHooks

    internal fun asyncTelemetry(): RuntimeAsyncTelemetry = RuntimeReference.Holder.value.asyncTelemetry

    internal fun databaseTracing(): RuntimeManualDatabaseTracing = RuntimeReference.Holder.value.manualDatabaseTracing

    internal fun ioTelemetry(): RuntimeIOTelemetry = RuntimeReference.Holder.value.ioTelemetry

    internal fun networkTelemetry(): RuntimeNetworkAdapterTelemetry = RuntimeReference.Holder.value.networkAdapterTelemetry

    internal fun workerTelemetry(): RuntimeWorkerTelemetry = RuntimeReference.Holder.value.workerTelemetry

    internal fun concurrencyTelemetry(): RuntimeConcurrencyTelemetry = RuntimeReference.Holder.value.concurrencyTelemetry

    internal fun manualTelemetry(): RuntimeManualTelemetry = RuntimeReference.Holder.value.manualTelemetry

    @JvmStatic
    fun init(context: Context?) {
        initializedRuntime().lifecycle.init(context)
    }

    /**
     * Idempotent, process-local bootstrap used by generated Android component hooks.
     * The one-shot CAS keeps every component invocation after the first one allocation-free.
     */
    @PublishedApi
    @JvmStatic
    internal fun autoInit(context: Context?) {
        initializedRuntime().lifecycle.autoInit(context)
    }

    @JvmStatic
    fun init(context: Context?, providedConfig: JankHunterConfig?) {
        initializedRuntime().lifecycle.init(context, providedConfig)
    }

    @JvmStatic
    fun isStarted(): Boolean = graph.lifecycle.isStarted()

    /** Returns the active process choice; configuration updates take effect in the next process. */
    @JvmStatic
    fun executionObservationModes(): JankHunterExecutionObservationModes = graph.state.observationModes()

    /** Requested strategies for the next process; the host owns persisting remote configuration. */
    @JvmStatic
    fun requestedExecutionObservationModes(): JankHunterExecutionObservationModes {
        val config = graph.state.config ?: return graph.state.observationModes()
        return JankHunterExecutionObservationModes(config.taskObservationMode(), config.coroutineObservationMode())
    }

    @JvmStatic
    fun isRuntimeEnabled(): Boolean = graph.lifecycle.isRuntimeEnabled()

    /**
     * Stores runtime intent independently of global/process restrictions. A true request remains
     * pending if those restrictions prevent startup; false is returned when startup is denied.
     * Disabling stops admission of new hook events before returning.
     */
    @JvmStatic
    @JvmOverloads
    fun setRuntimeEnabled(enabled: Boolean, reason: String? = "manual"): Boolean {
        return graph.lifecycle.setRuntimeEnabled(enabled, reason)
    }

    /**
     * Applies explicit changes to the last accepted configuration and restarts collection.
     * Omitted settings retain their values, including disabled features and storage policies.
     * The currently selected binary storage is retained unless the updater explicitly changes it.
     * Runtime enablement retains the last explicit request unless the updater sets it again.
     * Calls from a startup callback or the writer thread return false before acquiring control locks.
     */
    @JvmStatic
    @JvmOverloads
    fun reconfigure(
        reason: String? = "manual",
        updater: JankHunterConfigUpdater,
    ): Boolean {
        return graph.lifecycle.reconfigure(reason, updater)
    }

    /**
     * Atomically moves the active session to [storage] without restarting collection. Passing
     * `null` restores Jank Hunter's built-in file storage. The selected storage is retained while
     * runtime collection is disabled and is used by the next runtime start.
     * An external storage is rejected with [JankHunterStorageSwitchResult.FAILED] while an active
     * storagePolicy owns storage; clear that policy explicitly before selecting an external owner.
     * Startup/writer callbacks return FAILED to avoid waiting for their own lifecycle or writer work.
     * [JankHunterStorageSwitchResult.IN_PROGRESS] means the ordered switch already started and its
     * final state will be applied asynchronously.
     */
    @JvmStatic
    @JvmOverloads
    fun switchBinaryStorage(
        storage: JankHunterBinaryStorage?,
        timeoutMs: Long = DEFAULT_STORAGE_SWITCH_TIMEOUT_MS,
    ): JankHunterStorageSwitchResult {
        return graph.storageValve.switchBinaryStorage(storage, timeoutMs)
    }

    @JvmStatic
    fun initDiagnostics(): JankHunterInitDiagnostics = graph.lifecycle.diagnostics()

    /**
     * Captures immutable file copies without splitting the process recording into additional files.
     * Close the snapshot after consuming its paths to release compatibility copies from the app cache.
     * Returns `null` on the main thread or from the writer callback; use [captureLogSnapshotAsync] from UI code.
     */
    @JvmStatic
    fun captureLogSnapshot(): JankHunterLogSnapshot? = graph.session.captureLogSnapshot()

    /** Captures a snapshot on Jank Hunter's maintenance worker. */
    @JvmStatic
    fun captureLogSnapshotAsync(callback: JankHunterCaptureCallback<JankHunterLogSnapshot>): Boolean {
        return graph.session.captureLogSnapshotAsync(callback)
    }

    /**
     * Captures every live process at one vector frontier and atomically publishes one ZIP file.
     * Returns `null` on the main thread or from the writer callback; use [captureLogArchiveAsync] from UI code.
     */
    @JvmStatic
    fun captureLogArchive(destination: File): JankHunterLogArchive? {
        return graph.session.captureLogArchive(destination)
    }

    /** Captures and writes an archive on Jank Hunter's maintenance worker. */
    @JvmStatic
    fun captureLogArchiveAsync(
        destination: File,
        callback: JankHunterCaptureCallback<JankHunterLogArchive>,
    ): Boolean {
        return graph.session.captureLogArchiveAsync(destination, callback)
    }

    /**
     * Exports retained history plus the current coordinated frontier as one `.jhlog.zip` per
     * logical session. Source session directories and archives remain untouched.
     * Returns `null` on the main thread or from the writer callback; use [captureSessionArchivesAsync] from UI code.
     */
    @JvmStatic
    fun captureSessionArchives(destinationDirectory: File): List<String>? {
        return graph.session.captureSessionArchives(destinationDirectory)
    }

    /**
     * Exports session archives with content metadata verified while copying this snapshot.
     * Completed heap dumps remain inside their owning session archive. Call on a worker thread.
     */
    @JvmStatic
    fun captureSessionArchiveArtifacts(destinationDirectory: File): List<JankHunterSessionArchive>? {
        return graph.session.captureSessionArchiveArtifacts(destinationDirectory)
    }

    /** Exports session logs with optional managed heap dumps for transport size control. */
    @JvmStatic
    fun captureSessionArchives(destinationDirectory: File, includeHeapDumps: Boolean): List<String>? {
        return graph.session.captureSessionArchives(destinationDirectory, includeHeapDumps)
    }

    /**
     * Includes managed heap dumps only when all exported ZIPs fit [maxBytesIncludingHeapDumps].
     * Oversized payloads skip dumps before copying; ZIP overhead can trigger a logs-only retry.
     * JHLOG files are never truncated or omitted, even if they alone exceed this budget.
     * Source dumps remain unchanged. Hosts must reserve space for their other attachments.
     */
    @JvmStatic
    fun captureSessionArchives(destinationDirectory: File, maxBytesIncludingHeapDumps: Long): List<String>? {
        return graph.session.captureSessionArchives(destinationDirectory, maxBytesIncludingHeapDumps)
    }

    /** Exports session archives on Jank Hunter's maintenance worker. */
    @JvmStatic
    fun captureSessionArchivesAsync(
        destinationDirectory: File,
        callback: JankHunterCaptureCallback<List<String>>,
    ): Boolean {
        return graph.session.captureSessionArchivesAsync(destinationDirectory, callback)
    }

    /** Requests an asynchronous flush on the UI/writer thread; other callers share one bounded drain deadline. */
    @JvmStatic
    fun flush() {
        graph.session.flush()
    }

    @JvmStatic
    fun shutdown() {
        graph.lifecycle.shutdown()
    }

    private fun nowMs(): Long = SystemClock.elapsedRealtime()

    private fun nowUs(): Long = SystemClock.elapsedRealtimeNanos().coerceAtLeast(0L) / 1_000L

    private const val DEFAULT_STORAGE_SWITCH_TIMEOUT_MS = 5_000L
}
