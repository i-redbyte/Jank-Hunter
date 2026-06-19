package io.jankhunter.runtime

import android.app.ActivityManager
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import android.os.Handler
import android.os.SystemClock
import android.view.View
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.BinaryLogWriter
import io.jankhunter.runtime.internal.system.DeviceSnapshots
import io.jankhunter.runtime.internal.system.ProcessNames
import io.jankhunter.runtime.internal.system.RetainedHeapDumper
import java.io.File
import java.util.concurrent.Callable
import java.util.concurrent.Executor
import java.util.concurrent.ExecutorService
import java.util.concurrent.ScheduledExecutorService
import java.util.concurrent.ThreadPoolExecutor

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
    )
    private val sampling = RuntimeSamplingService(::nowMs)
    private val logSpam = RuntimeLogSpamService(
        ::nowMs,
        { config },
        { writer },
        { isRuntimeActiveForHooks() },
        { isAppForeground() },
        { ownerName -> captureContext(ownerOverride = firstContextValue(ownerName, contextTracker.ownerOrNull())) },
        { recordCounter("jankhunter.log_spam.dropped_keys.count", 1) },
    )
    private val runtimeCallGraph = RuntimeCallGraph(
        nowMs = ::nowMs,
        captureContext = { ownerOverride -> captureContext(ownerOverride = ownerOverride) },
        maxKeys = { config?.maxRuntimeCallGraphKeys() ?: DEFAULT_MAX_RUNTIME_CALL_GRAPH_KEYS },
    )
    private val handlerWrappers = HandlerWrapperRegistry { metricName ->
        recordCounter(metricName, 1)
    }

    private var writer: AsyncLogWriter?
        get() = runtimeState.writer
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
        init(context, JankHunterConfig.builder().build())
    }

    @JvmStatic
    fun init(context: Context?, providedConfig: JankHunterConfig?) {
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

        var acquiredStart = false
        var processNameForDiagnostics: String? = null
        var directoryForDiagnostics: File? = null
        try {
            val appContext = context.applicationContext ?: context
            val processName = ProcessNames.current(appContext)
            processNameForDiagnostics = processName
            val mainProcessName = appContext.packageName
            if (!providedConfig.isProcessAllowed(processName, mainProcessName)) {
                recordInitStatus("process_not_allowed", attempt, processName)
                return
            }
            if (!coordinator.tryMarkStarted()) {
                recordInitStatus("already_started", attempt, processName)
                return
            }
            acquiredStart = true

            config = providedConfig
            metrics.configure(providedConfig.maxMetricAggregationKeys())
            runtimeCallGraph.resetFlushState()
            sampling.configure(providedConfig)

            val directory = providedConfig.logDirectory() ?: File(appContext.filesDir, "jankhunter")
            directoryForDiagnostics = directory
            val redactedProcessName = providedConfig.redactProcessName(processName)
                ?.takeIf { it.isNotBlank() }
                ?: "unknown"
            val asyncWriter = AsyncLogWriter.open(
                directory,
                providedConfig,
                ProcessNames.safeFileSuffix(redactedProcessName, mainProcessName),
            )
            writer = asyncWriter

            val identity = appIdentity(appContext)
            val device = if (providedConfig.deviceInfoEnabled()) {
                DeviceSnapshots.current()
            } else {
                DeviceSnapshots.redacted()
            }
            asyncWriter.session(
                identity.versionName,
                identity.versionCode,
                device.displayName,
                Build.VERSION.SDK_INT,
                redactedProcessName,
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
            )

            collectors.start(appContext, providedConfig, directory)
            recordInitStatus("started", attempt, processName, directory)
        } catch (throwable: Throwable) {
            if (acquiredStart) {
                shutdown()
            } else {
                resetState()
            }
            recordInitFailure(throwable, attempt, processNameForDiagnostics, directoryForDiagnostics)
        }
    }

    @JvmStatic
    fun isStarted(): Boolean = started.get()

    @JvmStatic
    fun initDiagnostics(): JankHunterInitDiagnostics = initDiagnostics

    @JvmStatic
    fun lastInitFailure(): String? {
        val diagnostics = initDiagnostics
        return diagnostics.failureClass?.let { failureClass ->
            diagnostics.failureMessage?.let { "$failureClass: $it" } ?: failureClass
        }
    }

    @JvmStatic
    fun shutdown() {
        swallow {
            flushLogSpam(force = true)
            flushMetrics(force = true)
            runtimeCallGraph.flush(force = true, writer)
        }
        collectors.stop()
        swallow { writer?.close() }
        resetState()
    }

    private fun resetState() {
        collectors.reset()
        writer = null
        contextTracker.resetRecordedContext()
        metrics.reset()
        sampling.reset()
        logSpam.reset()
        runtimeCallGraph.clear()
        handlerWrappers.clear()
        coordinator.markStopped()
    }

    private inline fun swallow(block: () -> Unit) {
        try {
            block()
        } catch (_: Throwable) {
        }
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

    @JvmStatic
    fun withOwner(ownerName: String?, runnable: Runnable) {
        val start = nowMs()
        try {
            callWithOwner(ownerName) {
                runnable.run()
            }
        } finally {
            val duration = nowMs() - start
            if (duration >= 250) {
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
            if (duration >= 250) {
                recordStall(ownerName, "explicit_owner_block", duration)
            }
        }
    }

    @JvmStatic
    fun startFlow(flowName: String?): JankHunterFlow {
        val token = contextTracker.startFlow(flowName)
        ensureContextRecorded()
        return token
    }

    @JvmStatic
    fun endFlow(token: JankHunterFlow?) {
        contextTracker.endFlow(token)
        ensureContextRecorded()
    }

    @JvmStatic
    fun markFlowStep(stepName: String?) {
        contextTracker.markFlowStep(stepName)
        ensureContextRecorded()
    }

    @JvmStatic
    fun enterAnnotatedContext(
        screenName: String?,
        ownerName: String?,
        flowName: String?,
        traceName: String?,
    ): Any? {
        if (!isRuntimeActiveForHooks()) return null
        val token = contextTracker.enterScopedContext(screenName, ownerName, flowName, traceName)
        ensureContextRecorded()
        return token
    }

    @JvmStatic
    fun exitAnnotatedContext(token: Any?) {
        if (token !is JankHunterAnnotationScope) return
        contextTracker.exitScopedContext(token)
        ensureContextRecorded()
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
        return RuntimeDecoratorFactory.wrapRunnable(runnable, ownerName, isRuntimeActiveForHooks())
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
        if (!isRuntimeActiveForHooks()) return
        val wrappers = handlerWrappers.wrappers(handler, runnable, null)
        wrappers.forEach { handler.removeCallbacks(it) }
        handlerWrappers.unregister(handler, runnable, null)
    }

    @JvmStatic
    fun removeHandlerCallbacks(handler: Handler, runnable: Runnable, token: Any?) {
        handler.removeCallbacks(runnable, token)
        if (!isRuntimeActiveForHooks()) return
        val wrappers = handlerWrappers.wrappers(handler, runnable, token)
        wrappers.forEach { handler.removeCallbacks(it, token) }
        handlerWrappers.unregister(handler, runnable, token)
    }

    @JvmStatic
    fun removeHandlerCallbacksAndMessages(handler: Handler, token: Any?) {
        handler.removeCallbacksAndMessages(token)
        if (!isRuntimeActiveForHooks()) return
        handlerWrappers.unregister(handler, token)
    }

    @JvmStatic
    fun hasHandlerCallbacks(handler: Handler, runnable: Runnable): Boolean {
        if (Build.VERSION.SDK_INT < 29) return false
        if (handler.hasCallbacks(runnable)) return true
        if (!isRuntimeActiveForHooks()) return false
        return handlerWrappers.wrappers(handler, runnable, null).any { handler.hasCallbacks(it) }
    }

    internal fun unregisterHandlerRunnable(delegate: Runnable, wrapper: Runnable) {
        handlerWrappers.unregister(delegate, wrapper)
    }

    private fun wrapHandlerRunnable(
        handler: Handler,
        runnable: Runnable,
        token: Any?,
        ownerName: String?,
    ): Runnable {
        val runtimeActive = isRuntimeActiveForHooks()
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
        val wrapped = wrapHandlerRunnable(handler, runnable, token, ownerName)
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
        return RuntimeDecoratorFactory.wrapCallable(callable, ownerName, isRuntimeActiveForHooks())
    }

    @JvmStatic
    fun wrapCoroutineBlock(block: Function2<*, *, *>?, ownerName: String?): Function2<*, *, *>? {
        return RuntimeDecoratorFactory.wrapCoroutineBlock(block, ownerName, isRuntimeActiveForHooks())
    }

    @JvmStatic
    fun wrapClickListener(listener: View.OnClickListener?, ownerName: String?): View.OnClickListener? {
        return RuntimeDecoratorFactory.wrapClickListener(listener, ownerName, isRuntimeActiveForHooks())
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

    @JvmStatic
    fun setScreen(screenName: String?) {
        val screen = contextTracker.setScreen(screenName)
        writer?.screen(screen)
        ensureContextRecorded()
    }

    @JvmStatic
    fun flush() {
        flushLogSpam(force = true)
        flushMetrics(force = true)
        runtimeCallGraph.flush(force = true, writer)
        writer?.flushBlocking(flushTimeoutMs())
    }

    @JvmStatic
    fun enterMethod(ownerName: String?): Long {
        return runtimeCallGraph.enter(ownerName, writer != null)
    }

    @JvmStatic
    fun exitMethod(startMs: Long, ownerName: String?) {
        runtimeCallGraph.exit(startMs, ownerName, writer)
    }

    internal fun setAppForeground(foreground: Boolean) {
        runtimeState.appForeground.set(foreground)
    }

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
        val attributedOwner = firstContextValue(owner, contextTracker.ownerOrNull())
        ensureContextRecorded(ownerOverride = attributedOwner)
        writer?.http(
            attributedOwner,
            config?.redactRoute(route) ?: route,
            durationMs,
            dnsMs,
            connectMs,
            ttfbMs,
            statusClass,
            rxBytes,
            txBytes,
            flags or foregroundFlag(),
        )
        val failed = flags and BinaryLogWriter.FLAG_HTTP_FAILED != 0L || statusClass >= 5
        if (failed || durationMs >= SLOW_HTTP_THRESHOLD_MS) {
            recordProblemWindow("http_slow_or_failed", durationMs, 1, durationMs, attributedOwner)
        }
    }

    @JvmStatic
    fun recordStall(owner: String?, stackHint: String?, durationMs: Long) {
        val attributedOwner = firstContextValue(owner, contextTracker.ownerOrNull())
        ensureContextRecorded(ownerOverride = attributedOwner)
        writer?.stall(attributedOwner, stackHint, durationMs, foreground = isAppForeground())
        recordProblemWindow("main_thread_stall", durationMs, 1, durationMs, attributedOwner)
    }

    @JvmStatic
    fun recordMemory(pssKb: Long, javaHeapKb: Long, nativeHeapKb: Long) {
        if (!shouldRecordMemorySample(pssKb, javaHeapKb, nativeHeapKb)) {
            recordCounter("jankhunter.memory_sample.skipped.count", 1)
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
        val retainedOwner = firstContextValue(holder, className)
        val tuple = captureContext(ownerOverride = retainedOwner)
        ensureContextRecorded(screenOverride = tuple.screen, ownerOverride = tuple.owner)
        writer?.retained(tuple.screen, tuple.owner, tuple.flow, tuple.step, className, holder, ageMs, count, foreground = isAppForeground())
        recordProblemWindow("retained_object", ageMs, count.coerceAtLeast(1L), ageMs, retainedOwner)
        maybeDumpRetainedHeap(className, retainedOwner, ageMs, count)
    }

    internal fun recordWatchedRetained(
        className: String?,
        holder: String?,
        context: JankHunterContext?,
        ageMs: Long,
        count: Long,
    ) {
        if (context == null) {
            recordRetained(className, holder, ageMs, count)
            return
        }
        val retainedOwner = firstContextValue(firstContextValue(holder, context.owner), className)
        callWithContext(context, retainedOwner) {
            recordRetained(className, holder, ageMs, count)
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
            recordCounter("jankhunter.context_sample.skipped.count", 1)
            return
        }
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
        p50Ms: Long,
        p95Ms: Long,
        p99Ms: Long,
    ) {
        val attributedScreen = firstContextValue(screen, contextTracker.currentScreen())
        ensureContextRecorded(screenOverride = attributedScreen)
        writer?.uiWindow(attributedScreen, windowMs, frameCount, jankCount, p50Ms, p95Ms, p99Ms, foreground = isAppForeground())
        if (jankCount > 0 || p95Ms >= UI_PROBLEM_FRAME_THRESHOLD_MS) {
            recordProblemWindow("ui_jank", windowMs, jankCount.coerceAtLeast(1L), p95Ms)
        }
    }

    @JvmStatic
    fun recordCounter(name: String?, value: Long) {
        metrics.recordCounter(name, value)
    }

    @JvmStatic
    fun recordGauge(name: String?, value: Long) {
        metrics.recordGauge(name, value)
    }

    @JvmStatic
    fun recordLogSpam(ownerName: String?, source: String?, level: Int) {
        logSpam.record(ownerName, source, level)
    }

    internal fun captureContext(
        screenOverride: String? = null,
        ownerOverride: String? = null,
    ): JankHunterContext {
        return contextTracker.capture(screenOverride, ownerOverride)
    }

    internal fun <T> callWithOwner(ownerName: String?, block: () -> T): T {
        return callWithContext(captureContext(), ownerName, block)
    }

    internal fun <T> callWithContext(context: JankHunterContext, ownerName: String?, block: () -> T): T {
        return contextTracker.callWithContext(context, ownerName, ::ensureContextRecorded, block)
    }

    internal fun recordWrappedWork(ownerName: String?, kind: String, durationMs: Long, failed: Boolean) {
        val owner = metricOwner(ownerName)
        if (failed) {
            recordCounter("owner.$owner.$kind.failure.count", 1)
        }
        if (durationMs >= WRAPPED_WORK_GAUGE_THRESHOLD_MS) {
            recordGauge("owner.$owner.$kind.duration_ms", durationMs)
        }
        if (failed || durationMs >= WRAPPED_WORK_PROBLEM_THRESHOLD_MS) {
            recordProblemWindow("wrapped_$kind", durationMs, 1, durationMs, ownerName)
        }
    }

    internal fun recordExecutorWait(name: String?, ownerName: String?, waitMs: Long) {
        val executorName = metricExecutorName(name)
        if (waitMs > 0) {
            recordGauge("executor.$executorName.wait_ms", waitMs)
        }
        recordCounter("executor.$executorName.started.count", 1)
        ownerName?.takeIf { it.isNotBlank() }?.let {
            recordCounter("owner.${metricOwner(it)}.executor.started.count", 1)
        }
    }

    internal fun recordExecutorSnapshot(name: String?, executor: Executor, queued: Int) {
        val executorName = metricExecutorName(name)
        recordGauge("executor.$executorName.queue_depth", queued.toLong())
        if (executor is ThreadPoolExecutor) {
            recordGauge("executor.$executorName.active_count", executor.snapshotActiveCount().toLong())
            recordGauge("executor.$executorName.pool_size", executor.poolSize.toLong())
            recordGauge("executor.$executorName.completed_task_count", executor.completedTaskCount)
        }
    }

    internal fun runExecutorTask(
        name: String?,
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
            val executorName = metricExecutorName(name)
            val durationMs = clock() - start
            recordGauge("executor.$executorName.service_ms", durationMs)
            if (failed) {
                recordCounter("executor.$executorName.failure.count", 1)
            }
            recordWrappedWork(ownerName, "executor", durationMs, failed)
        }
    }

    internal fun <T> callExecutorTask(
        name: String?,
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
            val executorName = metricExecutorName(name)
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

    private fun isRuntimeActiveForHooks(): Boolean {
        return coordinator.isActiveForHooks()
    }

    private fun flushMetrics(force: Boolean) {
        metrics.flush(force)
    }

    private fun flushTimeoutMs(): Long {
        return maxOf(1000L, (config?.flushIntervalMs() ?: 0L) + 500L)
    }

    private fun maybeDumpRetainedHeap(className: String?, holder: String?, ageMs: Long, count: Long) {
        val asyncWriter = writer ?: return
        when (val result = retainedHeapDumper?.maybeDump(className, holder, ageMs, count)) {
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
            null -> Unit
        }
    }

    private fun ensureContextRecorded(
        screenOverride: String? = null,
        ownerOverride: String? = null,
    ) {
        val asyncWriter = writer ?: return
        val tuple = captureContext(screenOverride, ownerOverride)
        if (!contextTracker.shouldRecord(tuple)) return
        asyncWriter.flowContext(tuple.screen, tuple.owner, tuple.flow, tuple.step)
    }

    private fun flushLogSpam(force: Boolean) {
        logSpam.flush(force)
    }

    private fun nowMs(): Long = SystemClock.elapsedRealtime()

    private fun isAppForeground(): Boolean {
        if (runtimeState.appForeground.get()) return true
        return try {
            val info = ActivityManager.RunningAppProcessInfo()
            ActivityManager.getMyMemoryState(info)
            info.importance <= ActivityManager.RunningAppProcessInfo.IMPORTANCE_FOREGROUND
        } catch (_: Throwable) {
            false
        }
    }

    private fun foregroundFlag(): Long = if (isAppForeground()) BinaryLogWriter.FLAG_APP_FOREGROUND else 0L

    private fun metricOwner(ownerName: String?): String {
        return ownerName
            ?.takeIf { it.isNotBlank() }
            ?.replace(OWNER_WHITESPACE, "_")
            ?: "unknown"
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
        } catch (_: PackageManager.NameNotFoundException) {
            AppIdentity("unknown", "unknown")
        }
    }

    private data class AppIdentity(
        val versionName: String,
        val versionCode: String,
    )

    private const val WRAPPED_WORK_GAUGE_THRESHOLD_MS = 50L
    private const val WRAPPED_WORK_PROBLEM_THRESHOLD_MS = 250L
    private const val SLOW_HTTP_THRESHOLD_MS = 1000L
    private const val UI_PROBLEM_FRAME_THRESHOLD_MS = 32L
    private const val DEFAULT_MAX_METRIC_AGGREGATION_KEYS = 2048
    private const val DEFAULT_MAX_RUNTIME_CALL_GRAPH_KEYS = 4096
    private const val DEFAULT_MAX_HANDLER_TRACKING_ENTRIES = 4096
    private const val DEFAULT_MAX_HANDLER_WRAPPERS_PER_RUNNABLE = 32
    private val OWNER_WHITESPACE = Regex("\\s+")

}
