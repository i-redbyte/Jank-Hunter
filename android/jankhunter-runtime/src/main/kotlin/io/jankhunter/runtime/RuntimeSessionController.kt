package io.jankhunter.runtime

import android.content.Context
import android.os.Build
import androidx.core.content.pm.PackageInfoCompat
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.io.Jhlog
import io.jankhunter.runtime.internal.io.ProcessLogSnapshotCoordinator
import io.jankhunter.runtime.internal.io.QualityCounterId
import io.jankhunter.runtime.internal.monotonicDeadlineAfterMillis
import io.jankhunter.runtime.internal.system.DeviceSnapshots
import io.jankhunter.runtime.internal.system.ProcessNames
import io.jankhunter.runtime.internal.system.isRuntimeMainThread
import java.io.File
import java.util.concurrent.atomic.AtomicBoolean

internal class RuntimeSessionController(
    private val state: RuntimeState,
    private val coordinator: RuntimeCoordinator,
    private val metrics: RuntimeMetricsService,
    private val sampling: RuntimeSamplingService,
    private val hookEvents: RuntimeHookEventTransport,
    private val callGraph: RuntimeCallGraph,
    private val handlerHooks: RuntimeHandlerHooks,
    private val asyncTelemetry: RuntimeAsyncTelemetry,
    private val collectors: RuntimeCollectorService,
    private val writerFactory: AsyncLogWriterFactory,
    private val elapsedRealtimeMs: RuntimeLongSource,
) {
    private val crashDrainInProgress = AtomicBoolean()

    fun start(
        appContext: Context,
        config: JankHunterConfig,
        attempt: Long,
        processName: String,
    ): File {
        if (!coordinator.tryBeginStart()) {
            coordinator.recordInitStatus("already_started", attempt, processName)
            return logDirectory(appContext, config)
        }

        metrics.configure(config.maxMetricAggregationKeys(), config.exactEventCollectionEnabled())
        sampling.configure(config)

        val directory = logDirectory(appContext, config)
        val redactedProcessName = config.redactProcessName(processName)
            ?.trim()
            ?.takeIf(String::isNotEmpty)
            ?: "unknown"
        val rawRoster = when {
            config.allowedProcesses().isNotEmpty() ->
                ProcessNames.Roster(config.allowedProcesses(), declarationComplete = true)
            config.mainProcessOnly() ->
                ProcessNames.Roster(setOf(appContext.packageName), declarationComplete = true)
            else -> ProcessNames.declared(appContext)
        }
        val expectedProcesses = rawRoster.names.mapNotNullTo(linkedSetOf()) { name ->
            config.redactProcessName(name)?.trim()?.takeIf(String::isNotEmpty)
        }
        val rosterDeclarationComplete = rawRoster.declarationComplete &&
            expectedProcesses.size == rawRoster.names.size && expectedProcesses.isNotEmpty()
        state.snapshotExpectedProcessCount = expectedProcesses.size.coerceAtLeast(1)
        val writer = writerFactory.open(
            directory,
            config,
            redactedProcessName,
            expectedProcesses = expectedProcesses.ifEmpty { setOf(redactedProcessName) },
            rosterDeclarationComplete = rosterDeclarationComplete,
            onTerminalStop = { stoppedWriter, reason, failure ->
                onWriterTerminalStop(stoppedWriter, reason, failure, attempt, processName, directory)
            },
        )
        state.writer = writer
        if (!config.runtimeCallGraphEnabled()) {
            writer.recordQuality(QualityCounterId.RUNTIME_GRAPH_DISABLED)
        }

        writeSessionHeader(writer, appContext, config)
        if (config.runtimeCallGraphEnabled() || config.semanticTracingEnabled()) {
            callGraph.resetFlushState(writer)
        }
        hookEvents.start(writer)
        installCrashFlushHandler()
        recordRuntimeStartMetadata(writer, config, attempt)

        collectors.start(appContext, config, directory)
        state.logSnapshotCoordinator = try {
            ProcessLogSnapshotCoordinator.start(
                context = appContext,
                directory = directory,
                processName = redactedProcessName,
                captureLocal = ::captureProcessLogSnapshot,
            )
        } catch (throwable: Throwable) {
            RuntimeHookGuard.rethrowFatal(throwable)
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.RUNTIME_LIFECYCLE)
            null
        }
        if (!state.runtimeEnabled.get()) {
            stop(clearInit = false)
            recordRuntimeDisabledStatus()
            return directory
        }
        coordinator.recordInitStatus("started", attempt, processName, directory)
        coordinator.markStarted(config)
        return directory
    }

    fun stop(clearInit: Boolean) {
        val stopResources = coordinator.beginStop()
        if (stopResources) {
            val activeWriter = state.writer
            val shutdownDeadlineNs = monotonicDeadlineAfterMillis(BLOCKING_FLUSH_TIMEOUT_MS)
            RuntimeHookGuard.swallow { state.logSnapshotCoordinator?.close() }
            state.logSnapshotCoordinator = null
            RuntimeHookGuard.swallow { collectors.stopProducers(remainingTimeoutMs(shutdownDeadlineNs)) }
            RuntimeHookGuard.swallow { flushMetricsBlocking(remainingTimeoutMs(shutdownDeadlineNs)) }
            RuntimeHookGuard.swallow { hookEvents.stopAndFlush(remainingTimeoutMs(shutdownDeadlineNs)) }
            RuntimeHookGuard.swallow { callGraph.flushForShutdown(remainingTimeoutMs(shutdownDeadlineNs)) }
            RuntimeHookGuard.swallow { collectors.shutdownMaintenance(remainingTimeoutMs(shutdownDeadlineNs)) }
            if (activeWriter != null) {
                RuntimeHookGuard.swallow {
                    val drain = RuntimeWriterDrain(activeWriter, remainingTimeoutMs(shutdownDeadlineNs))
                    hookEvents.whenWriterDrained(activeWriter, drain::complete)
                    callGraph.whenWriterDrained(activeWriter, drain::complete)
                }
            }
        }
        RuntimeHookGuard.swallow { restoreCrashFlushHandler() }
        reset(clearInit)
    }

    fun flush() {
        flushMetricsBlocking()
        hookEvents.flushBlocking(BLOCKING_FLUSH_TIMEOUT_MS)
        callGraph.flushBlocking(BLOCKING_FLUSH_TIMEOUT_MS)
        writer?.flushBlocking(BLOCKING_FLUSH_TIMEOUT_MS)
    }

    fun requestFlush() {
        RuntimeHookGuard.run {
            if (!metrics.requestFlush()) writer?.flush()
        }
    }

    fun captureLogSnapshot(): JankHunterLogSnapshot? {
        if (isRuntimeMainThread()) return null
        return captureLogSnapshotBlocking()
    }

    fun captureLogSnapshotAsync(callback: JankHunterCaptureCallback<JankHunterLogSnapshot>): Boolean {
        val scheduler = state.maintenanceScheduler ?: return false
        return scheduler.execute { callback.onComplete(captureLogSnapshotBlocking()) }
    }

    private fun captureLogSnapshotBlocking(): JankHunterLogSnapshot? {
        val snapshotCoordinator = state.logSnapshotCoordinator
        return when {
            snapshotCoordinator != null -> snapshotCoordinator.capture()
            state.snapshotExpectedProcessCount <= 1 -> captureProcessLogSnapshot()
            else -> null
        }
    }

    fun captureLogArchive(destination: File): JankHunterLogArchive? {
        if (isRuntimeMainThread()) return null
        return captureLogArchiveBlocking(destination)
    }

    fun captureLogArchiveAsync(
        destination: File,
        callback: JankHunterCaptureCallback<JankHunterLogArchive>,
    ): Boolean {
        val scheduler = state.maintenanceScheduler ?: return false
        return scheduler.execute { callback.onComplete(captureLogArchiveBlocking(destination)) }
    }

    private fun captureLogArchiveBlocking(destination: File): JankHunterLogArchive? {
        val snapshot = captureLogSnapshotBlocking() ?: return null
        return try {
            JankHunterLogArchiveWriter.write(destination, snapshot)
        } catch (throwable: Throwable) {
            RuntimeHookGuard.rethrowFatal(throwable)
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.RUNTIME_LIFECYCLE)
            null
        }
    }

    fun logDirectory(appContext: Context, config: JankHunterConfig): File {
        return config.logDirectory() ?: File(appContext.filesDir, "jankhunter")
    }

    fun recordRuntimeDisabledStatus() {
        val appContext = state.initContext
        val processName = appContext?.let {
            try {
                ProcessNames.current(it)
            } catch (throwable: Throwable) {
                RuntimeHookGuard.rethrowFatal(throwable)
                null
            }
        }
        val directory = state.config?.logDirectory() ?: appContext?.filesDir?.let { File(it, "jankhunter") }
        coordinator.recordInitStatus("runtime_disabled", state.initAttempts.get(), processName, directory)
    }

    private fun writeSessionHeader(writer: AsyncLogWriter, context: Context, config: JankHunterConfig) {
        val identity = appIdentity(context)
        val device = DeviceSnapshots.current()
        val accepted = writer.session(
            identity.versionName,
            identity.versionCode,
            device.displayName,
            Build.VERSION.SDK_INT,
            device.androidRelease,
            device.securityPatch,
            device.primaryAbi,
            device.supportedAbis,
            device.manufacturer,
            device.brand,
            device.hardware,
            device.board,
            device.product,
            device.rooted,
            collectorFlags(config),
        )
        if (!accepted) {
            throw writer.terminalFailureCause()
                ?: IllegalStateException("Jank Hunter writer failed to start")
        }
    }

    private fun captureProcessLogSnapshot(timeoutMs: Long = BLOCKING_FLUSH_TIMEOUT_MS): JankHunterLogSnapshot? {
        val activeWriter = writer ?: return null
        val deadlineNs = monotonicDeadlineAfterMillis(timeoutMs.coerceAtLeast(1L))
        if (!flushMetricsBlocking(remainingTimeoutMs(deadlineNs))) return null
        if (!hookEvents.flushBlocking(remainingTimeoutMs(deadlineNs))) return null
        if (!callGraph.flushBlocking(remainingTimeoutMs(deadlineNs))) return null
        val snapshot = activeWriter.captureSnapshotBlocking(remainingTimeoutMs(deadlineNs)) ?: return null
        return JankHunterLogSnapshot(snapshot.capturedAtMs, snapshot.logPaths)
    }

    private fun onWriterTerminalStop(
        stoppedWriter: AsyncLogWriter,
        reason: Int,
        failure: Throwable?,
        attempt: Long,
        processName: String,
        logDirectory: File,
    ) {
        val terminalFailure = failure ?: IllegalStateException(terminalFailureMessage(reason))
        try {
            synchronized(state.lifecycleLock) {
                if (state.writer !== stoppedWriter) return
                stop(clearInit = false)
                coordinator.recordInitFailure(terminalFailure, attempt, processName, logDirectory)
            }
        } catch (throwable: Throwable) {
            RuntimeHookGuard.rethrowFatal(throwable)
            // Terminal recovery is diagnostic infrastructure and must remain fail-open.
        }
    }

    private fun terminalFailureMessage(reason: Int): String {
        return when (reason) {
            QualityCounterId.REASON_STORAGE_BUDGET -> "Jank Hunter stopped: storage_budget_exhausted"
            QualityCounterId.REASON_SIZE_LIMIT -> "Jank Hunter session log reached its configured size limit"
            else -> "Jank Hunter writer stopped after a terminal I/O failure"
        }
    }

    private fun reset(clearInit: Boolean) {
        state.writer = null
        RuntimeHookGuard.swallow { state.logSnapshotCoordinator?.close() }
        state.logSnapshotCoordinator = null
        state.snapshotExpectedProcessCount = 0
        RuntimeHookGuard.swallow { collectors.reset() }
        RuntimeHookGuard.swallow { metrics.reset() }
        RuntimeHookGuard.swallow { sampling.reset() }
        RuntimeHookGuard.swallow { hookEvents.clear() }
        RuntimeHookGuard.swallow { callGraph.clear() }
        RuntimeHookGuard.swallow { handlerHooks.clear() }
        RuntimeHookGuard.swallow { asyncTelemetry.clearCoroutineExecutions() }
        coordinator.markStopped()
        if (clearInit) {
            RuntimeHookGuard.run(RuntimeHookFailureReason.COLLECTOR) { state.activityObservation.close() }
            state.clearConfiguration()
            state.initContext = null
            state.collectionInactiveSinceElapsedMs.set(0L)
            state.runtimeEnabled.set(true)
        }
    }

    private fun installCrashFlushHandler() {
        val current = Thread.getDefaultUncaughtExceptionHandler()
        if (current === state.crashFlushHandler) return
        state.previousCrashHandler = current
        val handler = createCrashFlushHandler(current)
        state.crashFlushHandler = handler
        Thread.setDefaultUncaughtExceptionHandler(handler)
    }

    internal fun createCrashFlushHandler(previous: Thread.UncaughtExceptionHandler?): Thread.UncaughtExceptionHandler {
        val crashWriter = writer
        return RuntimeCrashFlushHandler(
            previous = previous,
            diagnostic = { name -> crashWriter?.recordCrashDiagnostic(name) },
            metrics = { timeoutMs -> metrics.flushBlocking(timeoutMs, crashWriter) },
            hooks = { timeoutMs -> state.writer === crashWriter && hookEvents.flushBlocking(timeoutMs) },
            graph = { timeoutMs -> state.writer === crashWriter && callGraph.flushBlocking(timeoutMs) },
            writer = { timeoutMs -> crashWriter?.flushBlocking(timeoutMs) ?: true },
            draining = crashDrainInProgress,
            requestWriterFlush = { crashWriter?.flush() },
        )
    }

    private fun restoreCrashFlushHandler() {
        val handler = state.crashFlushHandler
        if (handler != null && Thread.getDefaultUncaughtExceptionHandler() === handler) {
            Thread.setDefaultUncaughtExceptionHandler(state.previousCrashHandler)
        }
        state.crashFlushHandler = null
        state.previousCrashHandler = null
    }

    private fun recordRuntimeStartMetadata(writer: AsyncLogWriter, config: JankHunterConfig, attempt: Long) {
        writer.counter("jankhunter.runtime.session.start.count", 1)
        writer.gauge("jankhunter.runtime.init_attempt", attempt)
        val inactiveSince = state.collectionInactiveSinceElapsedMs.getAndSet(0L)
        if (inactiveSince > 0L) {
            val inactiveBeforeStartMs = (elapsedRealtimeMs.getAsLong() - inactiveSince).coerceAtLeast(0L)
            writer.gauge("jankhunter.runtime.collection_inactive_before_start_ms", inactiveBeforeStartMs)
        }
        val graphMode = if (config.runtimeCallGraphEnabled()) "exact" else "disabled"
        writer.counter("jankhunter.runtime_graph.mode.$graphMode.count", 1)
    }

    private fun flushMetricsBlocking(timeoutMs: Long = BLOCKING_FLUSH_TIMEOUT_MS): Boolean {
        val activeWriter = writer
        return metrics.flushBlocking(timeoutMs, activeWriter).also { succeeded ->
            if (!succeeded) activeWriter?.recordQuality(QualityCounterId.METRIC_FLUSH_TIMEOUT)
        }
    }

    private fun remainingTimeoutMs(deadlineNs: Long): Long {
        return ((deadlineNs - System.nanoTime()).coerceAtLeast(0L) / NANOS_PER_MILLISECOND).coerceAtLeast(1L)
    }

    private fun collectorFlags(config: JankHunterConfig): Long {
        var flags = Jhlog.COLLECTOR_MAIN_THREAD_STALLS
        if (config.fpsMonitorEnabled()) flags = flags or Jhlog.COLLECTOR_FPS
        if (config.jankStatsEnabled()) flags = flags or Jhlog.COLLECTOR_JANKSTATS
        if (config.processExitInfoEnabled()) flags = flags or Jhlog.COLLECTOR_PROCESS_EXIT
        if (config.ioTracingEnabled()) flags = flags or Jhlog.COLLECTOR_IO_TRACING
        if (config.systemSamplerEnabled()) flags = flags or Jhlog.COLLECTOR_SYSTEM_SAMPLER
        if (config.objectWatcherEnabled()) flags = flags or Jhlog.COLLECTOR_RETAINED_OBJECTS
        if (config.composeTracingEnabled()) flags = flags or Jhlog.COLLECTOR_COMPOSE
        if (config.roomTracingEnabled()) flags = flags or Jhlog.COLLECTOR_ROOM
        if (config.databaseTracingEnabled()) flags = flags or Jhlog.COLLECTOR_DATABASE
        if (config.workerTracingEnabled()) flags = flags or Jhlog.COLLECTOR_WORKER
        if (config.isRuntimeFeatureEnabled(JankHunterRuntimeFeature.HTTP)) flags = flags or Jhlog.COLLECTOR_HTTP
        return flags
    }

    private fun appIdentity(context: Context): AppIdentity {
        return try {
            val info = context.packageManager.getPackageInfo(context.packageName, 0)
            val versionName = info.versionName ?: "unknown"
            val versionCode = PackageInfoCompat.getLongVersionCode(info).toString()
            AppIdentity(versionName, versionCode)
        } catch (_: Exception) {
            AppIdentity("unknown", "unknown")
        }
    }

    private val writer: AsyncLogWriter?
        get() = state.writer?.takeIf { it.isAcceptingEvents() }

    private data class AppIdentity(val versionName: String, val versionCode: String)

    private companion object {
        const val BLOCKING_FLUSH_TIMEOUT_MS = 1_000L
        const val NANOS_PER_MILLISECOND = 1_000_000L
    }
}
