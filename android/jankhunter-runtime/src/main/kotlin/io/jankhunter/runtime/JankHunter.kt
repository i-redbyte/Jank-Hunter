package io.jankhunter.runtime

import android.app.ActivityManager
import android.app.Application
import android.content.Context
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import android.view.View
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.BinaryLogWriter
import io.jankhunter.runtime.internal.io.Jhlog
import io.jankhunter.runtime.internal.io.ProcessLogSnapshotCoordinator
import io.jankhunter.runtime.internal.io.QualityCounterId
import io.jankhunter.runtime.internal.system.DeviceSnapshots
import io.jankhunter.runtime.internal.system.ProcessNames
import io.jankhunter.runtime.internal.system.RetentionEvidence
import io.jankhunter.runtime.internal.system.RetainedLifecycleClassifier
import io.jankhunter.runtime.internal.system.RetainedHeapDumper
import io.jankhunter.runtime.internal.system.UiWindowClassifier
import java.io.File
import java.util.concurrent.Callable
import java.util.concurrent.Executor
import java.util.concurrent.ExecutorService
import java.util.concurrent.ScheduledExecutorService
import java.util.concurrent.ThreadPoolExecutor
import java.util.concurrent.atomic.AtomicBoolean

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

object JankHunter {
    private const val DEFAULT_RUNTIME_TOGGLE_REASON = "manual"

    private val autoInitAttempted = AtomicBoolean(false)
    @Volatile
    private var cachedProcessForeground = false
    @Volatile
    private var processForegroundCheckedAtMs = Long.MIN_VALUE
    private val runtimeState = RuntimeState()
    private val started get() = runtimeState.started
    private val initAttempts get() = runtimeState.initAttempts
    private val contextTracker = ContextTracker()
    private val coordinator = RuntimeCoordinator(runtimeState, ::nowMs)
    private val collectors = RuntimeCollectorService(runtimeState)
    private val metrics = RuntimeMetricsService(
        DEFAULT_MAX_METRIC_AGGREGATION_KEYS,
        ::nowMs,
        { writer },
        { config },
        { ensureContextRecorded() },
        { task -> runtimeState.maintenanceScheduler?.execute(task) == true },
        { delayMs, task -> runtimeState.maintenanceScheduler?.executeDelayed(delayMs, task) == true },
        { timeoutMs, task -> runtimeState.maintenanceScheduler?.executeAndWait(timeoutMs, task) == true },
    )
    private val sampling = RuntimeSamplingService(::nowMs)
    private val runtimeHookEvents = RuntimeHookEventTransport(
        maxCounterKeys = { config?.maxRuntimeCallGraphKeys() ?: DEFAULT_MAX_RUNTIME_CALL_GRAPH_KEYS },
        maxLogSpamKeys = { config?.maxLogSpamKeys() ?: DEFAULT_MAX_LOG_SPAM_KEYS },
        exactAdmission = { config?.exactEventCollectionEnabled() != false },
    )
    private val runtimeCallGraph = RuntimeCallGraph(
        nowMs = ::nowMs,
        captureScreen = contextTracker::currentScreenOrNull,
        captureFlow = contextTracker::currentFlowOrNull,
        captureStep = contextTracker::currentFlowStepOrNull,
        maxKeys = { config?.maxRuntimeCallGraphKeys() ?: DEFAULT_MAX_RUNTIME_CALL_GRAPH_KEYS },
        exactAdmission = { config?.exactEventCollectionEnabled() != false },
        admissionWaitNanos = {
            val activeConfig = config
            val waitMs = if (Thread.currentThread().name == "main") {
                activeConfig?.mainThreadAdmissionWaitMs() ?: 0L
            } else {
                activeConfig?.backgroundAdmissionWaitMs() ?: 5L
            }
            java.util.concurrent.TimeUnit.MILLISECONDS.toNanos(waitMs)
        },
    )
    private val handlerWrappers = HandlerWrapperRegistry(
        droppedCounter = { loss ->
            val qualityId = when (loss) {
                HandlerWrapperLoss.ENTRY_LIMIT -> QualityCounterId.HANDLER_ENTRY_LIMIT
                HandlerWrapperLoss.WRAPPER_LIMIT -> QualityCounterId.HANDLER_WRAPPER_LIMIT
                HandlerWrapperLoss.CONTENTION -> QualityCounterId.HANDLER_CONTENTION_BYPASS
            }
            writer?.recordQuality(qualityId)
        },
        exactAdmission = { config?.exactEventCollectionEnabled() != false },
    )

    private var writer: AsyncLogWriter?
        get() = runtimeState.writer?.takeIf { it.isAcceptingEvents() }
        set(value) {
            runtimeState.writer = value
        }

    private var config: JankHunterConfig?
        get() = runtimeState.config
        set(value) {
            runtimeState.config = value
        }

    private val objectRetentionWatcher
        get() = runtimeState.objectRetentionWatcher

    private val retainedHeapDumper
        get() = runtimeState.retainedHeapDumper

    private var initDiagnostics: JankHunterInitDiagnostics
        get() = runtimeState.initDiagnostics
        set(value) {
            runtimeState.initDiagnostics = value
        }

    @JvmStatic
    fun init(context: Context?) {
        val manifestConfig = context?.let { JankHunterConfig.fromManifest(it) }
            ?: JankHunterConfig.builder().build()
        synchronized(runtimeState.lifecycleLock) {
            initLocked(context, manifestConfig)
        }
    }

    /**
     * Idempotent, process-local bootstrap used by generated Android component hooks.
     * The one-shot CAS keeps every component invocation after the first one allocation-free.
     */
    @JvmStatic
    fun autoInit(context: Context?) {
        if (context == null) return
        if (!autoInitAttempted.compareAndSet(false, true)) return
        try {
            init(context)
        } catch (_: Throwable) {
            // Generated startup instrumentation must never take the host process down.
        }
    }

    @JvmStatic
    fun init(context: Context?, providedConfig: JankHunterConfig?) {
        val effectiveConfig = if (context != null && providedConfig != null) {
            JankHunterConfig.withBuildSymbolNamespace(
                providedConfig,
                JankHunterConfig.symbolNamespaceFromManifest(context),
            )
        } else {
            providedConfig
        }
        synchronized(runtimeState.lifecycleLock) {
            initLocked(context, effectiveConfig)
        }
    }

    private fun initLocked(context: Context?, providedConfig: JankHunterConfig?) {
        val attempt = initAttempts.incrementAndGet()
        if (context == null) {
            recordInitStatus("missing_context", attempt)
            return
        }
        if (providedConfig == null) {
            recordInitStatus("missing_config", attempt)
            return
        }
        if (!providedConfig.enabled()) {
            recordInitStatus("disabled", attempt)
            return
        }

        var processNameForDiagnostics: String? = null
        var directoryForDiagnostics: File? = null
        try {
            val appContext = runtimeContext(context)
            val processName = ProcessNames.current(appContext)
            processNameForDiagnostics = processName
            val mainProcessName = appContext.packageName
            if (!providedConfig.isProcessAllowed(processName, mainProcessName)) {
                recordInitStatus("process_not_allowed", attempt, processName)
                return
            }
            if (!coordinator.isStopped()) {
                recordInitStatus("already_started", attempt, processName)
                return
            }

            runtimeState.initContext = appContext
            config = providedConfig
            runtimeState.collectionInactiveSinceElapsedMs.compareAndSet(
                0L,
                SystemClock.elapsedRealtime(),
            )
            runtimeState.runtimeEnabled.set(providedConfig.runtimeEnabled())
            if (!providedConfig.runtimeEnabled()) {
                recordInitStatus("runtime_disabled", attempt, processName)
                return
            }
            directoryForDiagnostics = runtimeLogDirectory(appContext, providedConfig)
            directoryForDiagnostics = startRuntime(appContext, providedConfig, attempt, processName)
        } catch (throwable: Throwable) {
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.RUNTIME_LIFECYCLE)
            stopRuntime(clearInit = true)
            recordInitFailure(throwable, attempt, processNameForDiagnostics, directoryForDiagnostics)
        }
    }

    @JvmStatic
    fun isStarted(): Boolean = started.get()

    @JvmStatic
    fun isRuntimeEnabled(): Boolean = runtimeState.runtimeEnabled.get()

    @JvmStatic
    @JvmOverloads
    fun setRuntimeEnabled(enabled: Boolean, reason: String? = DEFAULT_RUNTIME_TOGGLE_REASON): Boolean {
        return synchronized(runtimeState.lifecycleLock) {
            setRuntimeEnabledLocked(enabled, reason)
        }
    }

    private fun setRuntimeEnabledLocked(enabled: Boolean, reason: String?): Boolean {
        runtimeState.runtimeEnabled.set(enabled)
        if (!enabled) {
            runtimeState.collectionInactiveSinceElapsedMs.set(SystemClock.elapsedRealtime())
            if (coordinator.isStarting()) {
                recordCounter("jankhunter.runtime.disabled.count", 1)
                recordRuntimeToggleReason("disabled", reason)
            } else if (!coordinator.isStopped()) {
                recordCounter("jankhunter.runtime.disabled.count", 1)
                recordRuntimeToggleReason("disabled", reason)
                stopRuntime(clearInit = false)
            }
            recordRuntimeDisabledStatus()
            return true
        }

        if (!coordinator.isStopped()) {
            return if (started.get()) {
                recordCounter("jankhunter.runtime.enabled.noop.count", 1)
                recordRuntimeToggleReason("enabled_noop", reason)
                true
            } else {
                recordInitStatus("runtime_enable_in_progress", initAttempts.get())
                false
            }
        }

        val appContext = runtimeState.initContext
        val currentConfig = config
        if (appContext == null || currentConfig == null || !currentConfig.enabled()) {
            recordInitStatus("runtime_enable_missing_init", initAttempts.get())
            return false
        }

        val attempt = initAttempts.incrementAndGet()
        var processNameForDiagnostics: String? = null
        var directoryForDiagnostics: File? = null
        return try {
            val processName = ProcessNames.current(appContext)
            processNameForDiagnostics = processName
            directoryForDiagnostics = runtimeLogDirectory(appContext, currentConfig)
            directoryForDiagnostics = startRuntime(appContext, currentConfig, attempt, processName)
            recordCounter("jankhunter.runtime.enabled.count", 1)
            recordRuntimeToggleReason("enabled", reason)
            requestFlush()
            true
        } catch (throwable: Throwable) {
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.RUNTIME_LIFECYCLE)
            stopRuntime(clearInit = false)
            recordInitFailure(throwable, attempt, processNameForDiagnostics, directoryForDiagnostics)
            false
        }
    }

    @JvmStatic
    fun initDiagnostics(): JankHunterInitDiagnostics = initDiagnostics

    @JvmStatic
    fun logGrowthSummary(): JankHunterLogGrowthSummary {
        runtimeState.writer?.logGrowthSummary()?.let { return it }
        val manager = runtimeState.logGrowthManager
        if (manager != null) {
            return runCatching(manager::summary).getOrElse {
                JankHunterLogGrowthSummary.disabled(nowMs())
            }
        }
        return JankHunterLogGrowthSummary.disabled(nowMs())
    }

    @JvmStatic
    fun writeLogGrowthSummary(): Boolean {
        if (config?.logGrowthAnalyticsEnabled() != true) return false
        val activeWriter = writer ?: return false
        if (!flushMetricsBlocking()) return false
        if (!runtimeHookEvents.flushBlocking(flushTimeoutMs())) return false
        if (!runtimeCallGraph.flushBlocking(flushTimeoutMs())) return false
        return activeWriter.writeLogGrowthSummaryBlocking(flushTimeoutMs())
    }

    /**
     * Seals a coordinated vector frontier in every live application process and immediately
     * continues collection in new segments. A null result means no trustworthy frontier was made.
     */
    @JvmStatic
    fun captureLogSnapshot(): JankHunterLogSnapshot? {
        val snapshotCoordinator = runtimeState.logSnapshotCoordinator
        return when {
            snapshotCoordinator != null -> snapshotCoordinator.capture()
            runtimeState.snapshotExpectedProcessCount <= 1 -> captureProcessLogSnapshot()
            else -> null
        }
    }

    /**
     * Captures every live process at one vector frontier and atomically publishes one ZIP file.
     * This performs file I/O and must be called from a background thread. Existing destinations
     * are never overwritten.
     */
    @JvmStatic
    fun captureLogArchive(destination: File): JankHunterLogArchive? {
        val snapshot = captureLogSnapshot() ?: return null
        return runCatching { JankHunterLogArchiveWriter.write(destination, snapshot) }.getOrNull()
    }

    private fun captureProcessLogSnapshot(): JankHunterLogSnapshot? {
        val activeWriter = writer ?: return null
        if (!flushMetricsBlocking()) return null
        if (!runtimeHookEvents.flushBlocking(flushTimeoutMs())) return null
        if (!runtimeCallGraph.flushBlocking(flushTimeoutMs())) return null
        val snapshot = activeWriter.captureSnapshotBlocking(flushTimeoutMs()) ?: return null
        return JankHunterLogSnapshot(snapshot.capturedAtMs, snapshot.logPaths)
    }

    @JvmStatic
    fun lastInitFailure(): String? {
        val diagnostics = initDiagnostics
        return diagnostics.failureClass?.let { failureClass ->
            diagnostics.failureMessage?.let { "$failureClass: $it" } ?: failureClass
        }
    }

    @JvmStatic
    fun shutdown() {
        synchronized(runtimeState.lifecycleLock) {
            stopRuntime(clearInit = true)
        }
    }

    private fun startRuntime(
        appContext: Context,
        providedConfig: JankHunterConfig,
        attempt: Long,
        processName: String,
    ): File {
        if (!coordinator.tryBeginStart()) {
            recordInitStatus("already_started", attempt, processName)
            return runtimeLogDirectory(appContext, providedConfig)
        }

        config = providedConfig
        metrics.configure(
            providedConfig.maxMetricAggregationKeys(),
            providedConfig.exactEventCollectionEnabled(),
        )
        sampling.configure(providedConfig)

        val directory = runtimeLogDirectory(appContext, providedConfig)
        val redactedProcessName = providedConfig.redactProcessName(processName)
            ?.trim()
            ?.takeIf(String::isNotEmpty)
            ?: "unknown"
        val rawRoster = when {
            providedConfig.allowedProcesses().isNotEmpty() ->
                ProcessNames.Roster(providedConfig.allowedProcesses(), declarationComplete = true)
            providedConfig.mainProcessOnly() ->
                ProcessNames.Roster(setOf(appContext.packageName), declarationComplete = true)
            else -> ProcessNames.declared(appContext)
        }
        val expectedProcesses = rawRoster.names.mapNotNullTo(linkedSetOf()) { name ->
            providedConfig.redactProcessName(name)?.trim()?.takeIf(String::isNotEmpty)
        }
        val rosterDeclarationComplete = rawRoster.declarationComplete &&
            expectedProcesses.size == rawRoster.names.size && expectedProcesses.isNotEmpty()
        runtimeState.snapshotExpectedProcessCount = expectedProcesses.size.coerceAtLeast(1)
        val asyncWriter = AsyncLogWriter.open(
            directory,
            providedConfig,
            redactedProcessName,
            expectedProcesses = expectedProcesses.ifEmpty { setOf(redactedProcessName) },
            rosterDeclarationComplete = rosterDeclarationComplete,
            onTerminalStop = { stoppedWriter, reason, failure ->
                onWriterTerminalStop(
                    stoppedWriter = stoppedWriter,
                    reason = reason,
                    failure = failure,
                    attempt = attempt,
                    processName = processName,
                    logDirectory = directory,
                )
            },
        )
        writer = asyncWriter
        runtimeState.logGrowthManager = asyncWriter.logGrowthManager()
        if (!providedConfig.runtimeCallGraphEnabled()) {
            asyncWriter.recordQuality(QualityCounterId.RUNTIME_GRAPH_DISABLED)
        }

        val identity = appIdentity(appContext)
        val device = DeviceSnapshots.current()
        val sessionAccepted = asyncWriter.session(
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
            collectorFlags(providedConfig),
        )
        if (!sessionAccepted) {
            throw asyncWriter.terminalFailureCause()
                ?: IllegalStateException("Jank Hunter writer failed to start")
        }
        if (providedConfig.runtimeCallGraphEnabled() || providedConfig.semanticTracingEnabled()) {
            runtimeCallGraph.resetFlushState(asyncWriter)
        }
        runtimeHookEvents.start(asyncWriter)
        installCrashFlushHandler()
        recordRuntimeStartMetadata(asyncWriter, attempt)

        collectors.start(appContext, providedConfig, directory)
        runtimeState.logSnapshotCoordinator = runCatching {
            ProcessLogSnapshotCoordinator.start(
                context = appContext,
                directory = directory,
                processName = redactedProcessName,
                captureLocal = ::captureProcessLogSnapshot,
            )
        }.getOrNull()
        if (!runtimeState.runtimeEnabled.get()) {
            stopRuntime(clearInit = false)
            recordRuntimeDisabledStatus()
            return directory
        }
        recordInitStatus("started", attempt, processName, directory)
        coordinator.markStarted()
        return directory
    }

    private fun onWriterTerminalStop(
        stoppedWriter: AsyncLogWriter,
        reason: Int,
        failure: Throwable?,
        attempt: Long,
        processName: String,
        logDirectory: File,
    ) {
        val terminalFailure = failure ?: IllegalStateException(
            if (reason == QualityCounterId.REASON_STORAGE_BUDGET) {
                "Jank Hunter stopped: storage_budget_exhausted"
            } else if (reason == QualityCounterId.REASON_SIZE_LIMIT) {
                "Jank Hunter session log reached its configured size limit"
            } else {
                "Jank Hunter writer stopped after a terminal I/O failure"
            },
        )
        try {
            synchronized(runtimeState.lifecycleLock) {
                // A delayed callback from an old session must never stop a successfully restarted
                // writer. The callback runs only after the writer has closed admission and its
                // loop has finished, so cleanup cannot self-join the writer thread.
                if (runtimeState.writer !== stoppedWriter) return
                stopRuntime(clearInit = false)
                recordInitFailure(terminalFailure, attempt, processName, logDirectory)
            }
        } catch (_: Throwable) {
            // Failure recovery is diagnostic infrastructure and must remain fail-open too.
        }
    }

    private fun stopRuntime(clearInit: Boolean) {
        val stopResources = coordinator.beginStop()
        if (stopResources) {
            val shutdownDeadlineNs = System.nanoTime() + BLOCKING_FLUSH_TIMEOUT_MS * NANOS_PER_MS
            RuntimeHookGuard.swallow { runtimeState.logSnapshotCoordinator?.close() }
            runtimeState.logSnapshotCoordinator = null
            RuntimeHookGuard.swallow { collectors.stop() }
            RuntimeHookGuard.swallow { flushMetricsBlocking(remainingShutdownTimeoutMs(shutdownDeadlineNs)) }
            RuntimeHookGuard.swallow { runtimeHookEvents.stopAndFlush(remainingShutdownTimeoutMs(shutdownDeadlineNs)) }
            RuntimeHookGuard.swallow { runtimeCallGraph.flushForShutdown() }
            RuntimeHookGuard.swallow { writer?.close(remainingShutdownTimeoutMs(shutdownDeadlineNs)) }
        }
        RuntimeHookGuard.swallow { restoreCrashFlushHandler() }
        resetRuntimeState(clearInit)
    }

    private fun resetRuntimeState(clearInit: Boolean) {
        writer = null
        RuntimeHookGuard.swallow { runtimeState.logSnapshotCoordinator?.close() }
        runtimeState.logSnapshotCoordinator = null
        runtimeState.snapshotExpectedProcessCount = 0
        RuntimeHookGuard.swallow { collectors.reset() }
        RuntimeHookGuard.swallow { metrics.reset() }
        RuntimeHookGuard.swallow { sampling.reset() }
        RuntimeHookGuard.swallow { runtimeHookEvents.clear() }
        RuntimeHookGuard.swallow { runtimeCallGraph.clear() }
        RuntimeHookGuard.swallow { handlerWrappers.clear() }
        coordinator.markStopped()
        if (clearInit) {
            config = null
            runtimeState.initContext = null
            runtimeState.collectionInactiveSinceElapsedMs.set(0L)
            runtimeState.runtimeEnabled.set(true)
        }
    }

    private fun installCrashFlushHandler() {
        val current = Thread.getDefaultUncaughtExceptionHandler()
        if (current === runtimeState.crashFlushHandler) {
            return
        }
        runtimeState.previousCrashHandler = current
        val previous = current
        val handler = Thread.UncaughtExceptionHandler { thread, throwable ->
            try {
                RuntimeHookGuard.swallow {
                    val crashWriter = writer
                    crashWriter?.counter("jankhunter.runtime.crash.count", 1)
                    crashWriter?.flushBlocking(
                        timeoutMs = CRASH_FLUSH_TIMEOUT_MS,
                        waitForExactFrontier = false,
                    )
                }
            } finally {
                previous?.uncaughtException(thread, throwable) ?: throw throwable
            }
        }
        runtimeState.crashFlushHandler = handler
        Thread.setDefaultUncaughtExceptionHandler(handler)
    }

    private fun restoreCrashFlushHandler() {
        val handler = runtimeState.crashFlushHandler
        if (handler != null && Thread.getDefaultUncaughtExceptionHandler() === handler) {
            Thread.setDefaultUncaughtExceptionHandler(runtimeState.previousCrashHandler)
        }
        runtimeState.crashFlushHandler = null
        runtimeState.previousCrashHandler = null
    }

    private fun recordInitStatus(
        status: String,
        attempt: Long,
        processName: String? = null,
        logDirectory: File? = null,
    ) {
        coordinator.recordInitStatus(status, attempt, processName, logDirectory)
    }

    private fun recordInitFailure(
        throwable: Throwable,
        attempt: Long,
        processName: String?,
        logDirectory: File?,
    ) {
        coordinator.recordInitFailure(throwable, attempt, processName, logDirectory)
    }

    private fun recordRuntimeDisabledStatus() {
        val appContext = runtimeState.initContext
        val processName = appContext?.let { runCatching { ProcessNames.current(it) }.getOrNull() }
        val directory = config?.logDirectory() ?: appContext?.filesDir?.let { File(it, "jankhunter") }
        recordInitStatus("runtime_disabled", initAttempts.get(), processName, directory)
    }

    private fun recordRuntimeToggleReason(state: String, reason: String?) {
        val cleanReason = reason
            ?.takeIf { it.isNotBlank() }
            ?.let(::metricOwner)
            ?: DEFAULT_RUNTIME_TOGGLE_REASON
        recordCounter("jankhunter.runtime.$state.reason.$cleanReason.count", 1)
    }

    private fun recordRuntimeStartMetadata(
        asyncWriter: AsyncLogWriter,
        attempt: Long,
    ) {
        asyncWriter.counter("jankhunter.runtime.session.start.count", 1)
        asyncWriter.gauge("jankhunter.runtime.init_attempt", attempt)
        val inactiveSince = runtimeState.collectionInactiveSinceElapsedMs.getAndSet(0L)
        if (inactiveSince > 0L) {
            val inactiveBeforeStartMs = (SystemClock.elapsedRealtime() - inactiveSince).coerceAtLeast(0L)
            asyncWriter.gauge(
                "jankhunter.runtime.collection_inactive_before_start_ms",
                inactiveBeforeStartMs,
            )
        }
        val graphMode = if (config?.runtimeCallGraphEnabled() == true) "exact" else "disabled"
        asyncWriter.counter("jankhunter.runtime_graph.mode.$graphMode.count", 1)
    }

    private fun runtimeLogDirectory(appContext: Context, providedConfig: JankHunterConfig): File {
        return providedConfig.logDirectory() ?: File(appContext.filesDir, "jankhunter")
    }

    private fun runtimeContext(context: Context): Context {
        return (context.applicationContext as? Application)
            ?: (context as? Application)
            ?: (context.applicationContext ?: context)
    }

    @JvmStatic
    fun withOwner(ownerName: String?, runnable: Runnable) {
        val start = nowMs()
        try {
            callWithOwner(ownerName) {
                runnable.run()
            }
        } finally {
            val duration = nowMs() - start
            if (shouldRecordOwnerStall(duration)) {
                recordStall(ownerName, "explicit_owner_block", duration)
            }
        }
    }

    @JvmStatic
    fun <T> withOwner(ownerName: String?, callable: Callable<T>): T {
        val start = nowMs()
        try {
            return callWithOwner(ownerName) {
                callable.call()
            }
        } finally {
            val duration = nowMs() - start
            if (shouldRecordOwnerStall(duration)) {
                recordStall(ownerName, "explicit_owner_block", duration)
            }
        }
    }

    @JvmStatic
    fun startFlow(flowName: String?): JankHunterFlow {
        val token = contextTracker.startFlow(flowName)
        RuntimeHookGuard.run { ensureContextRecorded() }
        return token
    }

    @JvmStatic
    fun endFlow(token: JankHunterFlow?) {
        contextTracker.endFlow(token)
        RuntimeHookGuard.run { ensureContextRecorded() }
    }

    @JvmStatic
    fun markFlowStep(stepName: String?) {
        contextTracker.markFlowStep(stepName)
        RuntimeHookGuard.run { ensureContextRecorded() }
    }

    @JvmStatic
    fun enterAnnotatedContext(
        screenName: String?,
        ownerName: String?,
        flowName: String?,
        traceName: String?,
    ): Any? {
        if (!isRuntimeActiveForCallbacks()) return null
        val token = RuntimeHookGuard.value<JankHunterAnnotationScope?>(null) {
            contextTracker.enterScopedContext(screenName, ownerName, flowName, traceName)
        }
        RuntimeHookGuard.run { ensureContextRecorded() }
        return token
    }

    @JvmStatic
    fun exitAnnotatedContext(token: Any?) {
        if (token !is JankHunterAnnotationScope) return
        RuntimeHookGuard.run { contextTracker.exitScopedContext(token) }
        RuntimeHookGuard.run { ensureContextRecorded() }
    }

    @JvmStatic
    fun withFlow(flowName: String?, runnable: Runnable) {
        val token = startFlow(flowName)
        try {
            runnable.run()
        } finally {
            endFlow(token)
        }
    }

    @JvmStatic
    fun <T> withFlow(flowName: String?, callable: Callable<T>): T {
        val token = startFlow(flowName)
        try {
            return callable.call()
        } finally {
            endFlow(token)
        }
    }

    @JvmStatic
    fun wrapRunnable(runnable: Runnable?, ownerName: String?): Runnable? {
        return RuntimeHookGuard.value(runnable) {
            RuntimeDecoratorFactory.wrapRunnable(runnable, ownerName, isRuntimeActiveForHooks())
        }
    }

    @JvmStatic
    fun postHandlerRunnable(handler: Handler, runnable: Runnable, ownerName: String?): Boolean {
        return postWrappedHandlerRunnable(handler, runnable, null, ownerName) {
            handler.post(it)
        }
    }

    @JvmStatic
    fun postHandlerRunnableAtFront(handler: Handler, runnable: Runnable, ownerName: String?): Boolean {
        return postWrappedHandlerRunnable(handler, runnable, null, ownerName) {
            handler.postAtFrontOfQueue(it)
        }
    }

    @JvmStatic
    fun postHandlerRunnableDelayed(
        handler: Handler,
        runnable: Runnable,
        delayMillis: Long,
        ownerName: String?,
    ): Boolean {
        return postWrappedHandlerRunnable(handler, runnable, null, ownerName) {
            handler.postDelayed(it, delayMillis)
        }
    }

    @JvmStatic
    fun postHandlerRunnableDelayed(
        handler: Handler,
        runnable: Runnable,
        token: Any?,
        delayMillis: Long,
        ownerName: String?,
    ): Boolean {
        return postWrappedHandlerRunnable(handler, runnable, token, ownerName) {
            handler.postAtTime(it, token, SystemClock.uptimeMillis() + delayMillis)
        }
    }

    @JvmStatic
    fun postHandlerRunnableAtTime(
        handler: Handler,
        runnable: Runnable,
        uptimeMillis: Long,
        ownerName: String?,
    ): Boolean {
        return postWrappedHandlerRunnable(handler, runnable, null, ownerName) {
            handler.postAtTime(it, uptimeMillis)
        }
    }

    @JvmStatic
    fun postHandlerRunnableAtTime(
        handler: Handler,
        runnable: Runnable,
        token: Any?,
        uptimeMillis: Long,
        ownerName: String?,
    ): Boolean {
        return postWrappedHandlerRunnable(handler, runnable, token, ownerName) {
            handler.postAtTime(it, token, uptimeMillis)
        }
    }

    @JvmStatic
    fun removeHandlerCallbacks(handler: Handler, runnable: Runnable) {
        handler.removeCallbacks(runnable)
        val wrappers = RuntimeHookGuard.value<List<Runnable>>(emptyList()) {
            handlerWrappers.wrappers(handler, runnable, null)
        }
        wrappers.forEach { wrapper ->
            RuntimeHookGuard.run { handler.removeCallbacks(wrapper) }
        }
        RuntimeHookGuard.run { handlerWrappers.unregister(handler, runnable, null) }
    }

    @JvmStatic
    fun removeHandlerCallbacks(handler: Handler, runnable: Runnable, token: Any?) {
        handler.removeCallbacks(runnable, token)
        val wrappers = RuntimeHookGuard.value<List<Runnable>>(emptyList()) {
            handlerWrappers.wrappers(handler, runnable, token)
        }
        wrappers.forEach { wrapper ->
            RuntimeHookGuard.run { handler.removeCallbacks(wrapper, token) }
        }
        RuntimeHookGuard.run { handlerWrappers.unregister(handler, runnable, token) }
    }

    @JvmStatic
    fun removeHandlerCallbacksAndMessages(handler: Handler, token: Any?) {
        handler.removeCallbacksAndMessages(token)
        RuntimeHookGuard.run { handlerWrappers.unregister(handler, token) }
    }

    @JvmStatic
    fun hasHandlerCallbacks(handler: Handler, runnable: Runnable): Boolean {
        if (Build.VERSION.SDK_INT < 29) return false
        if (handler.hasCallbacks(runnable)) return true
        val wrappers = RuntimeHookGuard.value<List<Runnable>>(emptyList()) {
            handlerWrappers.wrappers(handler, runnable, null)
        }
        return wrappers.any { wrapper ->
            RuntimeHookGuard.value(false) { handler.hasCallbacks(wrapper) }
        }
    }

    internal fun unregisterHandlerRunnable(delegate: Runnable, wrapper: Runnable) {
        RuntimeHookGuard.run { handlerWrappers.unregister(delegate, wrapper) }
    }

    internal fun onHandlerPostResult(original: Runnable, wrapped: Runnable, posted: Boolean) {
        if (!posted && wrapped !== original) {
            handlerWrappers.unregister(original, wrapped)
        }
    }

    internal fun handlerWrappers(handler: Handler, runnable: Runnable, token: Any?): Array<Runnable> {
        return handlerWrappers.wrappers(handler, runnable, token).toTypedArray()
    }

    internal fun clearHandlerWrappers(handler: Handler, runnable: Runnable, token: Any?) {
        handlerWrappers.unregister(handler, runnable, token)
    }

    internal fun clearHandlerWrappers(handler: Handler, token: Any?) {
        handlerWrappers.unregister(handler, token)
    }

    internal fun wrapHandlerRunnable(
        handler: Handler,
        runnable: Runnable,
        token: Any?,
        ownerName: String?,
    ): Runnable {
        val runtimeActive = isRuntimeActiveForCallbacks()
        val wrapper = RuntimeDecoratorFactory.wrapHandlerRunnable(runnable, ownerName, runtimeActive)
        if (wrapper === runnable) return runnable
        val maxEntries = config?.maxHandlerTrackingEntries() ?: DEFAULT_MAX_HANDLER_TRACKING_ENTRIES
        val maxWrappers = config?.maxHandlerWrappersPerRunnable() ?: DEFAULT_MAX_HANDLER_WRAPPERS_PER_RUNNABLE
        if (!handlerWrappers.register(handler, runnable, token, wrapper, maxEntries, maxWrappers)) {
            return runnable
        }
        return wrapper
    }

    private inline fun postWrappedHandlerRunnable(
        handler: Handler,
        runnable: Runnable,
        token: Any?,
        ownerName: String?,
        post: (Runnable) -> Boolean,
    ): Boolean {
        val wrapped = RuntimeHookGuard.value(runnable) {
            wrapHandlerRunnable(handler, runnable, token, ownerName)
        }
        try {
            val posted = post(wrapped)
            if (!posted && wrapped !== runnable) {
                unregisterHandlerRunnable(runnable, wrapped)
            }
            return posted
        } catch (throwable: Throwable) {
            if (wrapped !== runnable) {
                unregisterHandlerRunnable(runnable, wrapped)
            }
            throw throwable
        }
    }

    @JvmStatic
    fun <T> wrapCallable(callable: Callable<T>?, ownerName: String?): Callable<T>? {
        return RuntimeHookGuard.value(callable) {
            RuntimeDecoratorFactory.wrapCallable(callable, ownerName, isRuntimeActiveForHooks())
        }
    }

    @JvmStatic
    fun wrapCoroutineBlock(block: Function2<*, *, *>?, ownerName: String?): Function2<*, *, *>? {
        return RuntimeHookGuard.value(block) {
            RuntimeDecoratorFactory.wrapCoroutineBlock(block, ownerName, isRuntimeActiveForHooks())
        }
    }

    @JvmStatic
    fun wrapClickListener(listener: View.OnClickListener?, ownerName: String?): View.OnClickListener? {
        return RuntimeHookGuard.value(listener) {
            RuntimeDecoratorFactory.wrapClickListener(listener, ownerName, isRuntimeActiveForHooks())
        }
    }

    @JvmStatic
    fun wrapExecutor(executor: Executor?, name: String?, ownerName: String? = name): Executor? {
        if (executor == null ||
            executor is JankHunterExecutor ||
            executor is JankHunterExecutorService ||
            executor is JankHunterScheduledExecutorService
        ) {
            return executor
        }
        if (!isRuntimeActiveForHooks()) return executor
        return if (executor is ExecutorService) {
            wrapExecutorService(executor, name, ownerName)
        } else {
            JankHunterExecutor(executor, name, ownerName)
        }
    }

    @JvmStatic
    fun wrapExecutorService(executor: ExecutorService?, name: String?, ownerName: String? = name): ExecutorService? {
        if (executor == null ||
            executor is JankHunterExecutorService ||
            executor is JankHunterScheduledExecutorService
        ) {
            return executor
        }
        if (!isRuntimeActiveForHooks()) return executor
        return if (executor is ScheduledExecutorService) {
            JankHunterScheduledExecutorService(executor, name, ownerName)
        } else {
            JankHunterExecutorService(executor, name, ownerName)
        }
    }

    @JvmStatic
    fun wrapScheduledExecutorService(
        executor: ScheduledExecutorService?,
        name: String?,
        ownerName: String? = name,
    ): ScheduledExecutorService? {
        if (executor == null || executor is JankHunterScheduledExecutorService) return executor
        if (!isRuntimeActiveForHooks()) return executor
        return JankHunterScheduledExecutorService(executor, name, ownerName)
    }

    @JvmStatic
    fun currentOwner(): String = contextTracker.currentOwner()

    @JvmStatic
    fun currentScreen(): String = contextTracker.currentScreen()

    @JvmStatic
    fun currentFlow(): String = contextTracker.currentFlow()

    @JvmStatic
    fun currentFlowStep(): String = contextTracker.currentFlowStep()

    /** Captures attribution for asynchronous integrations such as network clients. */
    @JvmStatic
    fun captureContextSnapshot(): JankHunterContextSnapshot {
        val context = captureContext()
        return JankHunterContextSnapshot(context.screen, context.owner, context.flow, context.step)
    }

    @JvmStatic
    fun setScreen(screenName: String?) {
        contextTracker.setScreen(screenName)
        ensureContextRecorded()
    }

    @JvmStatic
    fun flush() {
        flushMetricsBlocking()
        runtimeHookEvents.flushBlocking(flushTimeoutMs())
        runtimeCallGraph.flushBlocking(flushTimeoutMs())
        writer?.flushBlocking(flushTimeoutMs())
    }

    @JvmStatic
    fun requestFlush() {
        RuntimeHookGuard.run {
            if (!metrics.requestFlush()) writer?.flush()
        }
    }

    @JvmStatic
    fun enterMethod(methodId: Long): Long {
        return enterMethod(methodId, null)
    }

    @JvmStatic
    fun enterMethod(methodId: Long, methodName: String?): Long {
        return RuntimeHookGuard.value(0L) {
            runtimeCallGraph.enter(
                methodId,
                methodName,
                isRuntimeActiveForHooks() && config?.runtimeCallGraphEnabled() == true,
            )
        }
    }

    @JvmStatic
    fun exitMethod(token: Long, methodId: Long) {
        RuntimeHookGuard.run { runtimeCallGraph.exit(token, methodId) }
    }

    @JvmStatic
    internal fun enterSemantic(kind: Int): Long {
        return RuntimeHookGuard.value(0L) {
            if (!isRuntimeActiveForHooks() || !semanticKindEnabled(kind)) 0L else {
                SystemClock.elapsedRealtimeNanos().coerceAtLeast(1L)
            }
        }
    }

    @JvmStatic
    internal fun exitSemantic(
        token: Long,
        kind: Int,
        methodId: Long,
        methodName: String?,
        outcome: Int,
    ) {
        RuntimeHookGuard.run {
            if (token == 0L || !semanticKindEnabled(kind)) return@run
            val durationNanos = (SystemClock.elapsedRealtimeNanos() - token).coerceAtLeast(0L)
            recordSemanticBoundary(
                kind = kind,
                calleeId = methodId,
                calleeName = methodName,
                durationNanos = durationNanos,
                outcome = workerOutcome(outcome),
            )
        }
    }

    @JvmStatic
    internal fun classifyWorkerOutcome(result: Any?): Int {
        return inferWorkerOutcome(result).code
    }

    @JvmStatic
    fun recordMethodCall(methodId: Long) {
        recordMethodCall(methodId, null)
    }

    @JvmStatic
    fun recordMethodCall(methodId: Long, methodName: String?) {
        RuntimeHookGuard.run {
            if (isRuntimeActiveForHooks()) runtimeHookEvents.recordMethod(methodId, methodName)
        }
    }

    internal fun setAppForeground(foreground: Boolean) {
        runtimeState.appForeground.set(foreground)
        runtimeState.memorySampler?.onForegroundChanged()
        runtimeState.systemContextSampler?.onForegroundChanged()
    }

    internal fun isAppForegroundForSampling(): Boolean = isAppForeground()

    @JvmStatic
    fun recordHttp(
        owner: String?,
        route: String?,
        durationMs: Long,
        dnsMs: Long,
        connectMs: Long,
        ttfbMs: Long,
        statusClass: Int,
        rxBytes: Long,
        txBytes: Long,
        flags: Long,
    ) {
        val context = captureContext(ownerOverride = firstContextValue(owner, contextTracker.ownerOrNull()))
        recordHttpWithContext(
            context,
            route,
            durationMs,
            dnsMs,
            connectMs,
            ttfbMs,
            statusClass,
            rxBytes,
            txBytes,
            flags,
        )
    }

    @JvmStatic
    fun recordHttpWithContextSnapshot(
        contextSnapshot: JankHunterContextSnapshot?,
        route: String?,
        durationMs: Long,
        dnsMs: Long,
        connectMs: Long,
        ttfbMs: Long,
        statusClass: Int,
        rxBytes: Long,
        txBytes: Long,
        flags: Long,
    ) {
        val context = contextSnapshot?.asRuntimeContext() ?: captureContext()
        recordHttpWithContext(
            context,
            route,
            durationMs,
            dnsMs,
            connectMs,
            ttfbMs,
            statusClass,
            rxBytes,
            txBytes,
            flags,
        )
    }

    private fun recordHttpWithContext(
        context: JankHunterContext,
        route: String?,
        durationMs: Long,
        dnsMs: Long,
        connectMs: Long,
        ttfbMs: Long,
        statusClass: Int,
        rxBytes: Long,
        txBytes: Long,
        flags: Long,
    ) {
        val classifiedFlags = flags or JankHunterNetworkEventFlags.HTTP_CLASSIFIED
        val effectiveFlags = if (durationMs >= httpSlowThresholdMs()) {
            classifiedFlags or JankHunterNetworkEventFlags.HTTP_SLOW
        } else {
            classifiedFlags
        }
        writer?.http(
            context.screen,
            context.owner,
            context.flow,
            context.step,
            config?.redactRoute(route) ?: route,
            durationMs,
            dnsMs,
            connectMs,
            ttfbMs,
            statusClass,
            rxBytes,
            txBytes,
            effectiveFlags or foregroundFlag(),
        )
    }

    @JvmStatic
    fun recordStall(owner: String?, stackHint: String?, durationMs: Long) {
        val attributedOwner = firstContextValue(owner, contextTracker.ownerOrNull())
        val context = captureContext(ownerOverride = attributedOwner)
        ensureContextRecorded(screenOverride = context.screen, ownerOverride = context.owner)
        writer?.stall(
            context.screen,
            context.owner,
            context.flow,
            context.step,
            stackHint,
            durationMs,
            foreground = isAppForeground(),
        )
    }

    internal fun captureMainThreadStallContext(owner: String?): JankHunterContextSnapshot {
        val mainContext = runtimeState.mainThreadContext
            ?: captureContext(ownerOverride = owner)
        if (runtimeState.heapDumpInProgress.get() || nowMs() <= runtimeState.heapDumpAttributionUntilMs.get()) {
            return JankHunterContextSnapshot(
                mainContext.screen,
                "jankhunter.heap_dump",
                "jankhunter.diagnostics",
                "heap_dump",
            )
        }
        return JankHunterContextSnapshot(
            mainContext.screen,
            firstContextValue(mainContext.owner, owner),
            mainContext.flow,
            mainContext.step,
        )
    }

    internal fun recordMainThreadStall(
        contextSnapshot: JankHunterContextSnapshot,
        stackHint: String?,
        durationMs: Long,
    ) {
        val mainContext = contextSnapshot.asRuntimeContext()
        writer?.stall(
            mainContext.screen,
            mainContext.owner,
            mainContext.flow,
            mainContext.step,
            stackHint,
            durationMs,
            foreground = isAppForeground(),
        )
    }

    @JvmStatic
    fun recordMemory(pssKb: Long, javaHeapKb: Long, nativeHeapKb: Long) {
        if (!shouldRecordMemorySample(pssKb, javaHeapKb, nativeHeapKb)) {
            return
        }
        ensureContextRecorded()
        writer?.memory(pssKb, javaHeapKb, nativeHeapKb, foreground = isAppForeground())
    }

    @JvmStatic
    fun recordRetained(className: String?, ageMs: Long, count: Long) {
        recordRetained(className, null, ageMs, count)
    }

    @JvmStatic
    fun recordRetained(className: String?, holder: String?, ageMs: Long, count: Long) {
        recordRetained(className, holder, ageMs, count, RetentionEvidence.TIME_ONLY)
    }

    private fun recordRetained(
        className: String?,
        holder: String?,
        ageMs: Long,
        count: Long,
        evidence: RetentionEvidence,
        attemptHeapDump: Boolean = true,
    ) {
        val retainedHolder = effectiveRetainedHolder(className, holder)
        val tuple = captureContext(ownerOverride = retainedHolder)
        ensureContextRecorded(screenOverride = tuple.screen, ownerOverride = tuple.owner)
        writer?.retained(
            tuple.screen,
            tuple.owner,
            tuple.flow,
            tuple.step,
            className,
            retainedHolder,
            ageMs,
            count,
            foreground = isAppForeground(),
            evidence = evidence,
        )
        if (attemptHeapDump) {
            maybeDumpRetainedHeap(className, retainedHolder, ageMs, count)
        }
    }

    internal fun recordWatchedRetained(
        className: String?,
        holder: String?,
        context: JankHunterContext?,
        ageMs: Long,
        count: Long,
        evidence: RetentionEvidence,
    ) {
        if (context == null) {
            recordRetained(className, holder, ageMs, count, evidence, attemptHeapDump = false)
            return
        }
        val explicitOrContextHolder = firstContextValue(holder, context.owner)
        val retainedHolder = effectiveRetainedHolder(className, explicitOrContextHolder)
        callWithContext(context, retainedHolder) {
            recordRetained(className, retainedHolder, ageMs, count, evidence, attemptHeapDump = false)
        }
    }

    internal fun dumpWatchedRetainedHeap(
        className: String?,
        holder: String?,
        context: JankHunterContext?,
        ageMs: Long,
        count: Long,
    ) {
        val retainedHolder = effectiveRetainedHolder(className, firstContextValue(holder, context?.owner))
        if (context == null) {
            maybeDumpRetainedHeap(className, retainedHolder, ageMs, count)
            return
        }
        callWithContext(context, retainedHolder) {
            maybeDumpRetainedHeap(className, retainedHolder, ageMs, count)
        }
    }

    @JvmStatic
    fun watchObject(instance: Any?, description: String? = null) {
        watchObject(instance, description, null)
    }

    @JvmStatic
    fun watchObject(instance: Any?, description: String?, ownerHint: String?) {
        val retainedBy = firstContextValue(ownerHint, contextTracker.ownerOrNull())
        val tuple = captureContext(ownerOverride = retainedBy)
        if (instance != null && objectRetentionWatcher != null) {
            recordCounter("jankhunter.object_watcher.watch.count", 1)
            if (retainedBy != null) {
                recordCounter("owner.${metricOwner(retainedBy)}.object_watcher.watch.count", 1)
            }
        }
        objectRetentionWatcher?.watch(instance, description, retainedBy, tuple)
    }

    @JvmStatic
    fun watchActivity(activity: android.app.Activity?) {
        watchActivity(activity, null)
    }

    @JvmStatic
    fun watchActivity(activity: android.app.Activity?, ownerHint: String?) {
        watchObject(activity, activity?.javaClass?.name, firstContextValue(ownerHint, activity?.javaClass?.name))
    }

    @JvmStatic
    fun watchFragment(fragmentLike: Any?, name: String? = null) {
        watchFragment(fragmentLike, name, null)
    }

    @JvmStatic
    fun watchFragment(fragmentLike: Any?, name: String?, ownerHint: String?) {
        watchObject(fragmentLike, name ?: fragmentLike?.javaClass?.name, ownerHint)
    }

    @JvmStatic
    fun watchView(view: View?) {
        watchView(view, null)
    }

    @JvmStatic
    fun watchView(view: View?, ownerHint: String?) {
        watchObject(view, view?.javaClass?.name, firstContextValue(ownerHint, view?.javaClass?.name))
    }

    @JvmStatic
    fun watchViewModel(viewModelLike: Any?, name: String? = null) {
        watchViewModel(viewModelLike, name, null)
    }

    @JvmStatic
    fun watchViewModel(viewModelLike: Any?, name: String?, ownerHint: String?) {
        watchObject(viewModelLike, name ?: viewModelLike?.javaClass?.name, ownerHint)
    }

    @JvmStatic
    fun watchService(service: android.app.Service?) {
        watchService(service, null)
    }

    @JvmStatic
    fun watchService(service: android.app.Service?, ownerHint: String?) {
        watchObject(service, service?.javaClass?.name, firstContextValue(ownerHint, service?.javaClass?.name))
    }

    @JvmStatic
    fun watchDialog(dialog: android.app.Dialog?) {
        watchDialog(dialog, null)
    }

    @JvmStatic
    fun watchDialog(dialog: android.app.Dialog?, ownerHint: String?) {
        watchObject(dialog, dialog?.javaClass?.name, firstContextValue(ownerHint, dialog?.javaClass?.name))
    }

    @JvmStatic
    fun watchLifecycleObject(instance: Any?, lifecycleEvent: String?, ownerHint: String?) {
        RuntimeHookGuard.run {
            watchLifecycleObjectUnsafe(instance, lifecycleEvent, ownerHint)
        }
    }

    private fun watchLifecycleObjectUnsafe(instance: Any?, lifecycleEvent: String?, ownerHint: String?) {
        val targets = RetainedLifecycleClassifier.targets(instance, lifecycleEvent, ownerHint)
        if (targets.isEmpty()) return
        for (target in targets) {
            withFlow(target.flow) {
                markFlowStep(target.step)
                watchObject(target.instance, target.description, target.ownerHint)
            }
        }
    }

    @JvmStatic
    fun watchCloseable(closeable: java.io.Closeable?, name: String? = null) {
        watchCloseable(closeable, name, null)
    }

    @JvmStatic
    fun watchCloseable(closeable: java.io.Closeable?, name: String?, ownerHint: String?) {
        watchObject(closeable, name ?: closeable?.javaClass?.name, ownerHint)
    }

    @JvmStatic
    fun recordContext(
        networkKind: Int,
        batteryPct: Int,
        availMemoryKb: Long,
        batteryState: Int,
        batteryTempDeciC: Int,
        lowMemory: Boolean,
        networkMetered: Boolean,
        networkValidated: Boolean,
        rxBytes: Long,
        txBytes: Long,
        totalMemoryKb: Long,
        freeStorageKb: Long,
        totalStorageKb: Long,
        networkVpn: Boolean,
    ) {
        if (
            !shouldRecordContextSample(
                networkKind,
                batteryPct,
                availMemoryKb,
                lowMemory,
                networkMetered,
                networkValidated,
                rxBytes,
                txBytes,
                networkVpn,
            )
        ) {
            return
        }
        ensureContextRecorded()
        writer?.context(
            networkKind,
            batteryPct,
            availMemoryKb,
            batteryState,
            batteryTempDeciC,
            lowMemory,
            networkMetered,
            networkValidated,
            rxBytes,
            txBytes,
            totalMemoryKb,
            freeStorageKb,
            totalStorageKb,
            networkVpn,
            foreground = isAppForeground(),
        )
    }

    @JvmStatic
    fun recordUiWindow(
        screen: String?,
        windowMs: Long,
        frameCount: Long,
        jankCount: Long,
        p95Ms: Long,
        source: Long,
        frameDeadlineUs: Long,
        frameDurationBuckets: LongArray,
    ) {
        val attributedScreen = firstContextValue(screen, contextTracker.currentScreen())
        ensureContextRecorded(screenOverride = attributedScreen)
        val flags = UiWindowClassifier.flags(jankCount, p95Ms, uiWindowP95ThresholdMs())
        writer?.uiWindow(
            attributedScreen,
            windowMs,
            frameCount,
            jankCount,
            source,
            frameDeadlineUs,
            frameDurationBuckets,
            foreground = isAppForeground(),
            flags = flags,
        )
    }

    @JvmStatic
    fun recordCounter(name: String?, value: Long) {
        RuntimeHookGuard.run { metrics.recordCounter(name, value) }
    }

    @JvmStatic
    fun recordGauge(name: String?, value: Long) {
        RuntimeHookGuard.run { metrics.recordGauge(name, value) }
    }

    /** Records one completed non-network I/O operation with atomic runtime attribution. */
    @JvmStatic
    @JvmOverloads
    fun recordIO(
        operation: JankHunterIOOperation,
        durationNanos: Long,
        bytes: Long = 0L,
        ownerName: String? = null,
    ) {
        if (config?.ioTracingEnabled() != true) return
        ensureContextRecorded(ownerOverride = firstContextValue(ownerName, contextTracker.ownerOrNull()))
        val mainLooper = Looper.getMainLooper()
        writer?.io(
            operation.wireValue,
            durationNanos.coerceAtLeast(0L) / NANOS_PER_MICROSECOND,
            bytes.coerceAtLeast(0L),
            mainLooper != null && Looper.myLooper() === mainLooper,
        )
    }

    /** Measures [block] with a monotonic clock and records it when manual I/O tracing is enabled. */
    @JvmStatic
    @JvmOverloads
    fun <T> traceIO(
        operation: JankHunterIOOperation,
        bytes: Long = 0L,
        ownerName: String? = null,
        block: () -> T,
    ): T {
        if (config?.ioTracingEnabled() != true) return block()
        val startedAt = SystemClock.elapsedRealtimeNanos()
        try {
            return block()
        } finally {
            recordIO(operation, SystemClock.elapsedRealtimeNanos() - startedAt, bytes, ownerName)
        }
    }

    /** Records an explicitly measured Compose composition/layout/draw phase. */
    @JvmStatic
    fun recordComposeWork(
        phase: JankHunterComposePhase,
        name: String,
        durationNanos: Long,
    ) {
        RuntimeHookGuard.run {
            if (!semanticKindEnabled(phase.semanticKind())) return@run
            recordNamedSemanticBoundary(phase.semanticKind(), name, durationNanos)
        }
    }

    /** Measures custom Compose work that cannot be classified safely from bytecode alone. */
    @JvmStatic
    fun <T> traceComposeWork(
        phase: JankHunterComposePhase,
        name: String,
        block: () -> T,
    ): T {
        if (!semanticKindEnabled(phase.semanticKind())) return block()
        val startedAt = SystemClock.elapsedRealtimeNanos()
        try {
            return block()
        } finally {
            recordComposeWork(phase, name, SystemClock.elapsedRealtimeNanos() - startedAt)
        }
    }

    /** Records a complete worker execution, including retry/cancellation outcomes. */
    @JvmStatic
    fun recordWorker(
        name: String,
        durationNanos: Long,
        outcome: JankHunterWorkerOutcome,
    ) {
        RuntimeHookGuard.run {
            if (!semanticKindEnabled(JankHunterSemanticWork.WORKER)) return@run
            recordNamedSemanticBoundary(JankHunterSemanticWork.WORKER, name, durationNanos, outcome)
        }
    }

    /** Measures a synchronous Worker/ListenableWorker body. */
    @JvmStatic
    fun <T> traceWorker(name: String, block: () -> T): T {
        if (!semanticKindEnabled(JankHunterSemanticWork.WORKER)) return block()
        val startedAt = SystemClock.elapsedRealtimeNanos()
        var outcome = JankHunterWorkerOutcome.UNKNOWN
        try {
            val result = block()
            outcome = inferWorkerOutcome(result)
            return result
        } catch (throwable: Throwable) {
            outcome = JankHunterWorkerOutcome.FAILURE
            throw throwable
        } finally {
            recordWorker(name, SystemClock.elapsedRealtimeNanos() - startedAt, outcome)
        }
    }

    /**
     * Measures a suspending CoroutineWorker body through its real completion, not only until the
     * first suspension point.
     */
    suspend fun <T> traceSuspendingWorker(name: String, block: suspend () -> T): T {
        if (!semanticKindEnabled(JankHunterSemanticWork.WORKER)) return block()
        val startedAt = SystemClock.elapsedRealtimeNanos()
        var outcome = JankHunterWorkerOutcome.UNKNOWN
        try {
            val result = block()
            outcome = inferWorkerOutcome(result)
            return result
        } catch (throwable: Throwable) {
            outcome = JankHunterWorkerOutcome.FAILURE
            throw throwable
        } finally {
            recordWorker(name, SystemClock.elapsedRealtimeNanos() - startedAt, outcome)
        }
    }

    private fun recordNamedSemanticBoundary(
        kind: Int,
        name: String,
        durationNanos: Long,
        outcome: JankHunterWorkerOutcome? = null,
    ) {
        val normalized = name.trim().takeIf(String::isNotEmpty) ?: "unknown"
        val targetId = JankHunterSemanticWork.stableId("jankhunter.semantic.target.v1\u0000$normalized")
        recordSemanticBoundary(kind, targetId, normalized, durationNanos, outcome)
    }

    private fun recordSemanticBoundary(
        kind: Int,
        calleeId: Long,
        calleeName: String?,
        durationNanos: Long,
        outcome: JankHunterWorkerOutcome?,
    ) {
        val mainLooper = Looper.getMainLooper()
        val mainThread = mainLooper != null && Looper.myLooper() === mainLooper
        val callerName = JankHunterSemanticWork.callerLabel(kind, mainThread, outcome) ?: return
        runtimeCallGraph.recordSemantic(
            callerId = JankHunterSemanticWork.stableId(callerName),
            callerName = callerName,
            calleeId = calleeId,
            calleeName = calleeName,
            durationMs = durationNanos.coerceAtLeast(0L) / NANOS_PER_MILLISECOND,
            enabled = isRuntimeActiveForHooks() && semanticKindEnabled(kind),
        )
    }

    private fun semanticKindEnabled(kind: Int): Boolean {
        val active = config ?: return false
        return when (kind) {
            JankHunterSemanticWork.COMPOSE_COMPOSITION,
            JankHunterSemanticWork.COMPOSE_MEASURE,
            JankHunterSemanticWork.COMPOSE_LAYOUT,
            JankHunterSemanticWork.COMPOSE_DRAW,
            -> active.composeTracingEnabled()
            JankHunterSemanticWork.ROOM_DAO -> active.roomTracingEnabled()
            JankHunterSemanticWork.WORKER -> active.workerTracingEnabled()
            else -> false
        }
    }

    private fun JankHunterComposePhase.semanticKind(): Int {
        return when (this) {
            JankHunterComposePhase.COMPOSITION -> JankHunterSemanticWork.COMPOSE_COMPOSITION
            JankHunterComposePhase.MEASURE -> JankHunterSemanticWork.COMPOSE_MEASURE
            JankHunterComposePhase.LAYOUT -> JankHunterSemanticWork.COMPOSE_LAYOUT
            JankHunterComposePhase.DRAW -> JankHunterSemanticWork.COMPOSE_DRAW
        }
    }

    private fun workerOutcome(value: Int): JankHunterWorkerOutcome? {
        return when (value) {
            JankHunterWorkerOutcome.SUCCESS.code -> JankHunterWorkerOutcome.SUCCESS
            JankHunterWorkerOutcome.FAILURE.code -> JankHunterWorkerOutcome.FAILURE
            JankHunterWorkerOutcome.RETRY.code -> JankHunterWorkerOutcome.RETRY
            JankHunterWorkerOutcome.CANCELLED.code -> JankHunterWorkerOutcome.CANCELLED
            else -> JankHunterWorkerOutcome.UNKNOWN
        }
    }

    private fun inferWorkerOutcome(result: Any?): JankHunterWorkerOutcome {
        val className = result?.javaClass?.name ?: return JankHunterWorkerOutcome.UNKNOWN
        return when {
            className.endsWith("${'$'}Success") -> JankHunterWorkerOutcome.SUCCESS
            className.endsWith("${'$'}Failure") -> JankHunterWorkerOutcome.FAILURE
            className.endsWith("${'$'}Retry") -> JankHunterWorkerOutcome.RETRY
            else -> JankHunterWorkerOutcome.SUCCESS
        }
    }

    internal fun recordQuality(counterId: Int, delta: Long = 1L) {
        writer?.recordQuality(counterId, delta)
    }

    internal fun recordProcessExit(
        reason: Long,
        timestampUnixMs: Long,
        importance: Long,
        pssKb: Long,
        rssKb: Long,
        processName: String?,
    ) {
        writer?.processExit(
            reason,
            timestampUnixMs,
            importance,
            pssKb,
            rssKb,
            config?.redactProcessName(processName) ?: processName,
        )
    }

    @JvmStatic
    fun recordLogSpam(ownerName: String?, source: String?, level: Int) {
        RuntimeHookGuard.run {
            if (!isRuntimeActiveForHooks()) return@run
            runtimeHookEvents.recordLogSpam(
                screen = contextTracker.currentScreenOrNull(),
                owner = ownerName?.takeIf { it.isNotBlank() } ?: contextTracker.ownerOrNull(),
                flow = contextTracker.currentFlowOrNull(),
                step = contextTracker.currentFlowStepOrNull(),
                source = source,
                level = level,
            )
        }
    }

    internal fun captureContext(
        screenOverride: String? = null,
        ownerOverride: String? = null,
    ): JankHunterContext {
        return contextTracker.capture(screenOverride, ownerOverride)
    }

    internal fun <T> callWithOwner(ownerName: String?, block: () -> T): T {
        val context = RuntimeHookGuard.value<JankHunterContext?>(null) { captureContext() }
        return if (context == null) block() else callWithContext(context, ownerName, block)
    }

    internal fun <T> callWithContext(context: JankHunterContext, ownerName: String?, block: () -> T): T {
        var delegateStarted = false
        return try {
            contextTracker.callWithContext(context, ownerName, ::ensureContextRecorded) {
                delegateStarted = true
                block()
            }
        } catch (throwable: Throwable) {
            if (throwable is VirtualMachineError || throwable is ThreadDeath) throw throwable
            if (delegateStarted) throw throwable
            block()
        }
    }

    internal fun recordWrappedWork(ownerName: String?, kind: String, durationMs: Long, failed: Boolean) {
        RuntimeHookGuard.run {
            recordWrappedWorkUnsafe(ownerName, kind, durationMs, failed)
        }
    }

    private fun recordWrappedWorkUnsafe(ownerName: String?, kind: String, durationMs: Long, failed: Boolean) {
        val owner = metricOwner(ownerName)
        if (failed) {
            recordCounter("owner.$owner.$kind.failure.count", 1)
        }
        if (durationMs >= WRAPPED_WORK_GAUGE_THRESHOLD_MS) {
            recordGauge("owner.$owner.$kind.duration_ms", durationMs)
        }
        if (failed || durationMs >= ownerBlockThresholdMs()) {
            recordProblemWindow("wrapped_$kind", durationMs, 1, durationMs, ownerName)
        }
    }

    internal fun recordExecutorWait(executorName: String, ownerName: String?, waitMs: Long) {
        if (waitMs > 0) {
            recordGauge("executor.$executorName.wait_ms", waitMs)
        }
        recordCounter("executor.$executorName.started.count", 1)
        ownerName?.takeIf { it.isNotBlank() }?.let {
            recordCounter("owner.${metricOwner(it)}.executor.started.count", 1)
        }
    }

    internal fun recordExecutorSnapshot(executorName: String, executor: Executor, queued: Int) {
        recordGauge("executor.$executorName.queue_depth", queued.toLong())
        if (executor is ThreadPoolExecutor) {
            recordGauge("executor.$executorName.active_count", executor.snapshotActiveCount().toLong())
            recordGauge("executor.$executorName.pool_size", executor.poolSize.toLong())
            recordGauge("executor.$executorName.completed_task_count", executor.completedTaskCount)
        }
    }

    internal fun runExecutorTask(
        executorName: String,
        ownerName: String?,
        command: Runnable,
        clock: () -> Long = ::nowMs,
    ) {
        val start = clock()
        var failed = false
        try {
            callWithOwner(ownerName) {
                command.run()
            }
        } catch (throwable: Throwable) {
            failed = true
            throw throwable
        } finally {
            val durationMs = clock() - start
            recordGauge("executor.$executorName.service_ms", durationMs)
            if (failed) {
                recordCounter("executor.$executorName.failure.count", 1)
            }
            recordWrappedWork(ownerName, "executor", durationMs, failed)
        }
    }

    internal fun <T> callExecutorTask(
        executorName: String,
        ownerName: String?,
        callable: Callable<T>,
        clock: () -> Long = ::nowMs,
    ): T {
        val start = clock()
        var failed = false
        try {
            return callWithOwner(ownerName) {
                callable.call()
            }
        } catch (throwable: Throwable) {
            failed = true
            throw throwable
        } finally {
            val durationMs = clock() - start
            recordGauge("executor.$executorName.service_ms", durationMs)
            if (failed) {
                recordCounter("executor.$executorName.failure.count", 1)
            }
            recordWrappedWork(ownerName, "executor", durationMs, failed)
        }
    }

    internal fun recordMainThreadDispatch(durationMs: Long, thresholdMs: Long, source: String?) {
        if (durationMs < thresholdMs) return
        recordGauge("main_thread.dispatch.duration_ms", durationMs)
        val overThresholdMs = durationMs - thresholdMs
        recordCounter("main_thread.dispatch.slow.count", 1)
        recordGauge("main_thread.dispatch.over_threshold_ms", overThresholdMs)
        recordCounter("screen.${metricOwner(currentScreen())}.main_thread.slow_dispatch.count", 1)
        recordCounter("main_thread.dispatch.source.${metricOwner(source)}.slow.count", 1)
        recordProblemWindow("main_thread_dispatch", durationMs, 1, durationMs, source)
    }

    internal fun recordClick(ownerName: String?, durationMs: Long, failed: Boolean) {
        recordWrappedWork(ownerName, "click", durationMs, failed)
    }

    private fun recordProblemWindow(
        kind: String,
        windowMs: Long,
        count: Long,
        maxMs: Long,
        ownerOverride: String? = null,
    ) {
        val tuple = captureContext(ownerOverride = firstContextValue(ownerOverride, contextTracker.ownerOrNull()))
        writer?.problemWindow(
            tuple.screen,
            tuple.owner,
            tuple.flow,
            tuple.step,
            kind,
            windowMs,
            count,
            maxMs,
            foreground = isAppForeground(),
        )
    }

    private fun shouldRecordMemorySample(pssKb: Long, javaHeapKb: Long, nativeHeapKb: Long): Boolean {
        return sampling.shouldRecordMemory(pssKb, javaHeapKb, nativeHeapKb)
    }

    private fun shouldRecordContextSample(
        networkKind: Int,
        batteryPct: Int,
        availMemoryKb: Long,
        lowMemory: Boolean,
        networkMetered: Boolean,
        networkValidated: Boolean,
        rxBytes: Long,
        txBytes: Long,
        networkVpn: Boolean,
    ): Boolean {
        return sampling.shouldRecordContext(
            networkKind,
            batteryPct,
            availMemoryKb,
            lowMemory,
            networkMetered,
            networkValidated,
            rxBytes,
            txBytes,
            networkVpn,
        )
    }

    internal fun isRuntimeActiveForCallbacks(): Boolean {
        return RuntimeHookGuard.value(false) { isRuntimeActiveForHooks() }
    }

    private fun isRuntimeActiveForHooks(): Boolean {
        return coordinator.isActiveForHooks()
    }

    private fun flushMetricsBlocking(timeoutMs: Long = flushTimeoutMs()): Boolean {
        return metrics.flushBlocking(timeoutMs).also { succeeded ->
            if (succeeded) return@also
            writer?.recordQuality(QualityCounterId.METRIC_FLUSH_TIMEOUT)
        }
    }

    private fun remainingShutdownTimeoutMs(deadlineNs: Long): Long {
        return ((deadlineNs - System.nanoTime()).coerceAtLeast(0L) / NANOS_PER_MS).coerceAtLeast(1L)
    }

    private fun flushTimeoutMs(): Long {
        return BLOCKING_FLUSH_TIMEOUT_MS
    }

    private fun maybeDumpRetainedHeap(className: String?, holder: String?, ageMs: Long, count: Long) {
        val asyncWriter = writer ?: return
        val heapDumper = retainedHeapDumper ?: return
        runtimeState.heapDumpInProgress.set(true)
        val result = try {
            heapDumper.maybeDump(className, holder, ageMs, count)
        } finally {
            val thresholdMs = config?.mainThreadStallThresholdMs() ?: HEAP_DUMP_ATTRIBUTION_MIN_MS
            val graceMs = maxOf(HEAP_DUMP_ATTRIBUTION_MIN_MS, thresholdMs * 2L)
            runtimeState.heapDumpAttributionUntilMs.set(nowMs() + graceMs)
            runtimeState.heapDumpInProgress.set(false)
        }
        when (result) {
            is RetainedHeapDumper.Result.Dumped -> {
                asyncWriter.counter("jankhunter.heap_dump.created.count", 1)
                asyncWriter.gauge("jankhunter.heap_dump.retained_age_ms", result.ageMs)
                asyncWriter.counter("jankhunter.heap_dump.retained_objects.count", result.count)
                asyncWriter.gauge("jankhunter.heap_dump.file_size_kb", result.file.length() / 1024L)
            }
            is RetainedHeapDumper.Result.Skipped -> {
                asyncWriter.counter("jankhunter.heap_dump.skipped.${metricOwner(result.reason)}.count", 1)
            }
            is RetainedHeapDumper.Result.Failed -> {
                asyncWriter.counter("jankhunter.heap_dump.failed.${metricOwner(result.reason)}.count", 1)
            }
        }
    }

    private fun ensureContextRecorded(
        screenOverride: String? = null,
        ownerOverride: String? = null,
    ) {
        val asyncWriter = writer ?: return
        val tuple = captureContext(screenOverride, ownerOverride)
        val mainLooper = Looper.getMainLooper()
        if (mainLooper != null && Looper.myLooper() === mainLooper) {
            runtimeState.mainThreadContext = tuple
        }
        asyncWriter.updateProducerContext(tuple.screen, tuple.owner, tuple.flow, tuple.step)
    }

    private fun nowMs(): Long = SystemClock.elapsedRealtime()

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
        if (config.workerTracingEnabled()) flags = flags or Jhlog.COLLECTOR_WORKER
        return flags
    }

    private fun shouldRecordOwnerStall(durationMs: Long): Boolean {
        val mainLooper = Looper.getMainLooper() ?: return false
        return isMainThreadOwnerBlock(
            durationMs = durationMs,
            thresholdMs = ownerBlockThresholdMs(),
            isMainThread = Looper.myLooper() === mainLooper,
            monitorActive = runtimeState.watchdog != null,
        )
    }

    private fun isAppForeground(): Boolean {
        if (runtimeState.appForeground.get()) return true
        // Once lifecycle tracking is installed its false state is authoritative. Avoid allocating
        // RunningAppProcessInfo and crossing into the framework on every sampled event.
        if (runtimeState.activityTracker != null) return false
        val checkedAtMs = processForegroundCheckedAtMs
        val nowMs = nowMs()
        if (checkedAtMs != Long.MIN_VALUE && nowMs - checkedAtMs in 0 until PROCESS_FOREGROUND_CACHE_MS) {
            return cachedProcessForeground
        }
        val foreground = RuntimeHookGuard.value(false, RuntimeHookFailureReason.COLLECTOR) {
            val info = ActivityManager.RunningAppProcessInfo()
            ActivityManager.getMyMemoryState(info)
            info.importance <= ActivityManager.RunningAppProcessInfo.IMPORTANCE_FOREGROUND
        }
        cachedProcessForeground = foreground
        processForegroundCheckedAtMs = nowMs
        return foreground
    }

    private fun foregroundFlag(): Long = if (isAppForeground()) BinaryLogWriter.FLAG_APP_FOREGROUND else 0L

    private fun ownerBlockThresholdMs(): Long = config?.ownerBlockThresholdMs() ?: DEFAULT_OWNER_BLOCK_THRESHOLD_MS

    private fun httpSlowThresholdMs(): Long = config?.httpSlowThresholdMs() ?: DEFAULT_HTTP_SLOW_THRESHOLD_MS

    private fun uiWindowP95ThresholdMs(): Long = config?.uiWindowP95ThresholdMs() ?: DEFAULT_UI_WINDOW_P95_THRESHOLD_MS

    private fun metricOwner(ownerName: String?): String {
        return ownerName
            ?.takeIf { it.isNotBlank() }
            ?.replace(OWNER_WHITESPACE, "_")
            ?: "unknown"
    }

    internal fun effectiveRetainedHolder(className: String?, holder: String?): String? {
        return firstContextValue(holder, className)
    }

    private fun appIdentity(context: Context): AppIdentity {
        return try {
            val info = context.packageManager.getPackageInfo(context.packageName, 0)
            val versionName = info.versionName ?: "unknown"
            val versionCode = if (Build.VERSION.SDK_INT >= 28) {
                info.longVersionCode.toString()
            } else {
                @Suppress("DEPRECATION")
                info.versionCode.toString()
            }
            AppIdentity(versionName, versionCode)
        } catch (_: Exception) {
            AppIdentity("unknown", "unknown")
        }
    }

    private data class AppIdentity(
        val versionName: String,
        val versionCode: String,
    )

    private const val WRAPPED_WORK_GAUGE_THRESHOLD_MS = 50L
    private const val DEFAULT_OWNER_BLOCK_THRESHOLD_MS = 250L
    private const val DEFAULT_UI_WINDOW_P95_THRESHOLD_MS = 32L
    private const val DEFAULT_HTTP_SLOW_THRESHOLD_MS = 1_000L
    private const val PROCESS_FOREGROUND_CACHE_MS = 1_000L
    private const val DEFAULT_MAX_METRIC_AGGREGATION_KEYS = 2048
    private const val DEFAULT_MAX_RUNTIME_CALL_GRAPH_KEYS = 4096
    private const val DEFAULT_MAX_LOG_SPAM_KEYS = 2048
    private const val DEFAULT_MAX_HANDLER_TRACKING_ENTRIES = 4096
    private const val DEFAULT_MAX_HANDLER_WRAPPERS_PER_RUNNABLE = 32
    private const val BLOCKING_FLUSH_TIMEOUT_MS = 1_000L
    private const val HEAP_DUMP_ATTRIBUTION_MIN_MS = 500L
    private const val CRASH_FLUSH_TIMEOUT_MS = 100L
    private const val NANOS_PER_MS = 1_000_000L
    private const val NANOS_PER_MICROSECOND = 1_000L
    private const val NANOS_PER_MILLISECOND = 1_000_000L
    private val OWNER_WHITESPACE = Regex("\\s+")

}
