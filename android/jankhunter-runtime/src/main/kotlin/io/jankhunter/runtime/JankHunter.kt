package io.jankhunter.runtime

import android.app.Application
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import android.os.Handler
import android.os.SystemClock
import android.view.View
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.MetricAggregator
import io.jankhunter.runtime.internal.system.AdaptiveRuntimeSampler
import io.jankhunter.runtime.internal.system.ActivityTracker
import io.jankhunter.runtime.internal.system.DeviceSnapshots
import io.jankhunter.runtime.internal.system.FpsMonitor
import io.jankhunter.runtime.internal.system.MainLooperDispatchMonitor
import io.jankhunter.runtime.internal.system.MainThreadWatchdog
import io.jankhunter.runtime.internal.system.MemorySampler
import io.jankhunter.runtime.internal.system.MemoryTrimReporter
import io.jankhunter.runtime.internal.system.ObjectRetentionWatcher
import io.jankhunter.runtime.internal.system.ProcessNames
import io.jankhunter.runtime.internal.system.ProcessExitReporter
import io.jankhunter.runtime.internal.system.RetainedHeapDumper
import io.jankhunter.runtime.internal.system.SystemContextSampler
import java.io.File
import java.lang.ref.WeakReference
import java.util.concurrent.Callable
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Executor
import java.util.concurrent.ExecutorService
import java.util.concurrent.ScheduledExecutorService
import java.util.concurrent.ThreadPoolExecutor
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong

object JankHunter {
    private val started = AtomicBoolean(false)
    private val owner = ThreadLocal<String>()
    private val flow = ThreadLocal<String>()
    private val flowStep = ThreadLocal<String>()
    private val contextLock = Any()
    private val logSpamCounters = ConcurrentHashMap<LogSpamKey, AtomicLong>()
    private val lastLogSpamFlushAtMs = AtomicLong(0L)
    private val lastMetricFlushAtMs = AtomicLong(0L)
    private val runtimeCallStack = ThreadLocal<MutableList<RuntimeCallFrame>>()
    private val runtimeCallCounters = ConcurrentHashMap<RuntimeCallKey, RuntimeCallStats>()
    private val runtimeCallDropped = AtomicLong(0L)
    private val lastRuntimeCallFlushAtMs = AtomicLong(0L)
    private val handlerRunnableLock = Any()
    private val handlerRunnableEntries = mutableListOf<HandlerRunnableEntry>()

    @Volatile
    private var writer: AsyncLogWriter? = null

    @Volatile
    private var config: JankHunterConfig? = null

    @Volatile
    private var metricAggregator = MetricAggregator(DEFAULT_MAX_METRIC_AGGREGATION_KEYS)

    @Volatile
    private var watchdog: MainThreadWatchdog? = null

    @Volatile
    private var dispatchMonitor: MainLooperDispatchMonitor? = null

    @Volatile
    private var memorySampler: MemorySampler? = null

    @Volatile
    private var memoryTrimReporter: MemoryTrimReporter? = null

    @Volatile
    private var componentCallbackContext: Context? = null

    @Volatile
    private var systemContextSampler: SystemContextSampler? = null

    @Volatile
    private var adaptiveRuntimeSampler: AdaptiveRuntimeSampler? = null

    @Volatile
    private var objectRetentionWatcher: ObjectRetentionWatcher? = null

    @Volatile
    private var retainedHeapDumper: RetainedHeapDumper? = null

    @Volatile
    private var fpsMonitor: FpsMonitor? = null

    @Volatile
    private var application: Application? = null

    @Volatile
    private var activityTracker: ActivityTracker? = null

    @Volatile
    private var screen = "unknown"

    @Volatile
    private var lastContextKey = ""

    @JvmStatic
    fun init(context: Context?) {
        init(context, JankHunterConfig.builder().build())
    }

    @JvmStatic
    fun init(context: Context?, providedConfig: JankHunterConfig?) {
        if (context == null || providedConfig == null || !providedConfig.enabled()) return

        var acquiredStart = false
        try {
            val appContext = context.applicationContext ?: context
            val processName = ProcessNames.current(appContext)
            val mainProcessName = appContext.packageName
            if (!providedConfig.isProcessAllowed(processName, mainProcessName)) return
            if (!started.compareAndSet(false, true)) return
            acquiredStart = true

            config = providedConfig
            metricAggregator = MetricAggregator(providedConfig.maxMetricAggregationKeys())
            lastMetricFlushAtMs.set(0L)
            lastRuntimeCallFlushAtMs.set(0L)
            runtimeCallDropped.set(0L)
            adaptiveRuntimeSampler = AdaptiveRuntimeSampler(
                providedConfig.adaptiveMemoryStableIntervalMs(),
                providedConfig.adaptiveContextStableIntervalMs(),
            )

            val directory = providedConfig.logDirectory() ?: File(appContext.filesDir, "jankhunter")
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
            val device = DeviceSnapshots.current()
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

            if (providedConfig.autoStartCollectors()) {
                if (appContext is Application) {
                    application = appContext
                    activityTracker = ActivityTracker(providedConfig.jankStatsEnabled()).also {
                        appContext.registerActivityLifecycleCallbacks(it)
                    }
                }
                watchdog = MainThreadWatchdog(providedConfig.mainThreadStallThresholdMs()).also { it.start() }
                if (providedConfig.mainLooperDispatchMonitorEnabled()) {
                    dispatchMonitor = MainLooperDispatchMonitor(providedConfig.mainThreadStallThresholdMs()).also {
                        it.start()
                    }
                }
                memoryTrimReporter = MemoryTrimReporter().also {
                    appContext.registerComponentCallbacks(it)
                    componentCallbackContext = appContext
                }
                memorySampler = MemorySampler(appContext, providedConfig.memorySampleIntervalMs()).also { it.start() }
                if (providedConfig.systemSamplerEnabled()) {
                    systemContextSampler = SystemContextSampler(
                        appContext,
                        providedConfig.systemSampleIntervalMs(),
                    ).also { it.start() }
                }
                if (providedConfig.processExitInfoEnabled()) {
                    ProcessExitReporter.report(appContext)
                }
                if (providedConfig.objectWatcherEnabled()) {
                    if (providedConfig.retainedHeapDumpEnabled()) {
                        retainedHeapDumper = RetainedHeapDumper(
                            providedConfig.retainedHeapDumpDirectory() ?: File(directory, "heap-dumps"),
                            providedConfig.retainedHeapDumpMinIntervalMs(),
                            providedConfig.retainedHeapDumpMaxCount(),
                            providedConfig.retainedHeapDumpMinRetainedAgeMs(),
                        )
                    }
                    objectRetentionWatcher = ObjectRetentionWatcher(
                        providedConfig.retainedObjectDelayMs(),
                        providedConfig.retainedObjectForceGcEnabled(),
                    ).also { it.start() }
                }
                if (providedConfig.fpsMonitorEnabled()) {
                    fpsMonitor = FpsMonitor(
                        providedConfig.fpsWindowMs(),
                        providedConfig.jankFrameThresholdMs(),
                    ).also { it.start() }
                }
            }
        } catch (_: Throwable) {
            if (acquiredStart) {
                shutdown()
            } else {
                resetState()
            }
        }
    }

    @JvmStatic
    fun isStarted(): Boolean = started.get()

    @JvmStatic
    fun shutdown() {
        swallow {
            flushLogSpam(force = true)
            flushMetrics(force = true)
            flushRuntimeCalls(force = true)
        }
        swallow {
            activityTracker?.let { tracker ->
                application?.unregisterActivityLifecycleCallbacks(tracker)
                tracker.close()
            }
        }
        swallow { watchdog?.stop() }
        swallow { dispatchMonitor?.stop() }
        swallow {
            memoryTrimReporter?.let { reporter ->
                componentCallbackContext?.unregisterComponentCallbacks(reporter)
            }
        }
        swallow { memorySampler?.stop() }
        swallow { systemContextSampler?.stop() }
        swallow { objectRetentionWatcher?.stop() }
        swallow { fpsMonitor?.stop() }
        swallow { writer?.close() }
        resetState()
    }

    private fun resetState() {
        activityTracker = null
        application = null
        watchdog = null
        dispatchMonitor = null
        memoryTrimReporter = null
        componentCallbackContext = null
        memorySampler = null
        systemContextSampler = null
        adaptiveRuntimeSampler = null
        objectRetentionWatcher = null
        retainedHeapDumper = null
        fpsMonitor = null
        writer = null
        lastContextKey = ""
        lastMetricFlushAtMs.set(0L)
        logSpamCounters.clear()
        runtimeCallCounters.clear()
        runtimeCallStack.remove()
        runtimeCallDropped.set(0L)
        synchronized(handlerRunnableLock) {
            handlerRunnableEntries.clear()
        }
        started.set(false)
    }

    private inline fun swallow(block: () -> Unit) {
        try {
            block()
        } catch (_: Throwable) {
        }
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
        val token = JankHunterFlow(
            previousFlow = flow.get(),
            previousStep = flowStep.get(),
        )
        setThreadLocal(flow, normalizedContextValue(flowName))
        flowStep.remove()
        ensureContextRecorded()
        return token
    }

    @JvmStatic
    fun endFlow(token: JankHunterFlow?) {
        if (token == null) return
        setThreadLocal(flow, token.previousFlow)
        setThreadLocal(flowStep, token.previousStep)
        ensureContextRecorded()
    }

    @JvmStatic
    fun markFlowStep(stepName: String?) {
        setThreadLocal(flowStep, normalizedContextValue(stepName))
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
        if (runnable == null || runnable is JankHunterRunnable) return runnable
        if (hasAdditionalTypeContract(runnable, Runnable::class.java)) return runnable
        return JankHunterRunnable(runnable, ownerName)
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
            handler.postDelayed(it, token, delayMillis)
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
        val wrappers = handlerWrappers(handler, runnable, null)
        wrappers.forEach { handler.removeCallbacks(it) }
        unregisterHandlerRunnables(handler, runnable, null)
    }

    @JvmStatic
    fun removeHandlerCallbacks(handler: Handler, runnable: Runnable, token: Any?) {
        handler.removeCallbacks(runnable, token)
        val wrappers = handlerWrappers(handler, runnable, token)
        wrappers.forEach { handler.removeCallbacks(it, token) }
        unregisterHandlerRunnables(handler, runnable, token)
    }

    @JvmStatic
    fun removeHandlerCallbacksAndMessages(handler: Handler, token: Any?) {
        handler.removeCallbacksAndMessages(token)
        unregisterHandlerRunnables(handler, token)
    }

    @JvmStatic
    fun hasHandlerCallbacks(handler: Handler, runnable: Runnable): Boolean {
        if (handler.hasCallbacks(runnable)) return true
        return handlerWrappers(handler, runnable, null).any { handler.hasCallbacks(it) }
    }

    internal fun unregisterHandlerRunnable(delegate: Runnable, wrapper: Runnable) {
        synchronized(handlerRunnableLock) {
            cleanHandlerRunnableEntriesLocked()
            val iterator = handlerRunnableEntries.iterator()
            while (iterator.hasNext()) {
                val entry = iterator.next()
                if (entry.original.get() === delegate) {
                    entry.wrappers.removeAll { it.wrapper.get() == null || it.wrapper.get() === wrapper }
                }
                if (entry.wrappers.isEmpty()) {
                    iterator.remove()
                }
            }
        }
    }

    private fun wrapHandlerRunnable(
        handler: Handler,
        runnable: Runnable,
        token: Any?,
        ownerName: String?,
    ): Runnable {
        if (runnable is JankHunterHandlerRunnable || runnable is JankHunterRunnable) return runnable
        val wrapper = JankHunterHandlerRunnable(runnable, ownerName)
        if (!registerHandlerRunnable(handler, runnable, token, wrapper)) {
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

    private fun registerHandlerRunnable(
        handler: Handler,
        runnable: Runnable,
        token: Any?,
        wrapper: Runnable,
    ): Boolean {
        synchronized(handlerRunnableLock) {
            cleanHandlerRunnableEntriesLocked()
            val maxEntries = config?.maxHandlerTrackingEntries() ?: DEFAULT_MAX_HANDLER_TRACKING_ENTRIES
            val maxWrappers = config?.maxHandlerWrappersPerRunnable() ?: DEFAULT_MAX_HANDLER_WRAPPERS_PER_RUNNABLE
            val entry = handlerRunnableEntries.firstOrNull {
                it.handler.get() === handler && it.original.get() === runnable
            }
            if (entry == null && (maxEntries <= 0 || handlerRunnableEntries.size >= maxEntries)) {
                recordCounter("jankhunter.handler_wrapper.dropped_entries.count", 1)
                return false
            }
            val resolvedEntry = entry ?: HandlerRunnableEntry(
                handler = WeakReference(handler),
                original = WeakReference(runnable),
                wrappers = mutableListOf(),
            ).also { handlerRunnableEntries.add(it) }
            if (maxWrappers <= 0 || resolvedEntry.wrappers.size >= maxWrappers) {
                recordCounter("jankhunter.handler_wrapper.dropped_wrappers.count", 1)
                return false
            }
            resolvedEntry.wrappers.add(
                HandlerRunnableWrapperEntry(
                    wrapper = WeakReference(wrapper),
                    token = token?.let(::WeakReference),
                ),
            )
            return true
        }
    }

    private fun handlerWrappers(handler: Handler, runnable: Runnable, token: Any?): List<Runnable> {
        synchronized(handlerRunnableLock) {
            cleanHandlerRunnableEntriesLocked()
            return handlerRunnableEntries
                .firstOrNull { it.handler.get() === handler && it.original.get() === runnable }
                ?.wrappers
                ?.filter { tokenMatches(it.token, token) }
                ?.mapNotNull { it.wrapper.get() }
                ?: emptyList()
        }
    }

    private fun unregisterHandlerRunnables(handler: Handler, runnable: Runnable, token: Any?) {
        synchronized(handlerRunnableLock) {
            cleanHandlerRunnableEntriesLocked()
            val iterator = handlerRunnableEntries.iterator()
            while (iterator.hasNext()) {
                val entry = iterator.next()
                if (entry.handler.get() === handler && entry.original.get() === runnable) {
                    entry.wrappers.removeAll { tokenMatches(it.token, token) }
                }
                if (entry.wrappers.isEmpty()) {
                    iterator.remove()
                }
            }
        }
    }

    private fun unregisterHandlerRunnables(handler: Handler, token: Any?) {
        synchronized(handlerRunnableLock) {
            cleanHandlerRunnableEntriesLocked()
            val iterator = handlerRunnableEntries.iterator()
            while (iterator.hasNext()) {
                val entry = iterator.next()
                if (entry.handler.get() === handler) {
                    entry.wrappers.removeAll { tokenMatches(it.token, token) }
                }
                if (entry.wrappers.isEmpty()) {
                    iterator.remove()
                }
            }
        }
    }

    private fun cleanHandlerRunnableEntriesLocked() {
        val iterator = handlerRunnableEntries.iterator()
        while (iterator.hasNext()) {
            val entry = iterator.next()
            if (entry.handler.get() == null || entry.original.get() == null) {
                iterator.remove()
            } else {
                entry.wrappers.removeAll { it.wrapper.get() == null }
                if (entry.wrappers.isEmpty()) {
                    iterator.remove()
                }
            }
        }
    }

    private fun tokenMatches(registeredToken: WeakReference<Any>?, requestedToken: Any?): Boolean {
        return requestedToken == null || registeredToken?.get() === requestedToken
    }

    @JvmStatic
    fun <T> wrapCallable(callable: Callable<T>?, ownerName: String?): Callable<T>? {
        if (callable == null || callable is JankHunterCallable<*>) return callable
        if (hasAdditionalTypeContract(callable, Callable::class.java)) return callable
        return JankHunterCallable(callable, ownerName)
    }

    @JvmStatic
    fun wrapCoroutineBlock(block: Function2<*, *, *>?, ownerName: String?): Function2<*, *, *>? {
        if (block == null || block is JankHunterCoroutineFunction2) return block
        @Suppress("UNCHECKED_CAST")
        return JankHunterCoroutineFunction2(block as Function2<Any?, Any?, Any?>, ownerName)
    }

    @JvmStatic
    fun wrapClickListener(listener: View.OnClickListener?, ownerName: String?): View.OnClickListener? {
        if (listener == null || listener is JankHunterClickListener) return listener
        return JankHunterClickListener(listener, ownerName)
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
        return JankHunterScheduledExecutorService(executor, name, ownerName)
    }

    @JvmStatic
    fun currentOwner(): String = owner.get() ?: "unknown"

    @JvmStatic
    fun currentScreen(): String = screen

    @JvmStatic
    fun currentFlow(): String = flow.get() ?: "unknown"

    @JvmStatic
    fun currentFlowStep(): String = flowStep.get() ?: "unknown"

    @JvmStatic
    fun setScreen(screenName: String?) {
        screen = screenName?.takeIf { it.isNotEmpty() } ?: "unknown"
        writer?.screen(screen)
        ensureContextRecorded()
    }

    @JvmStatic
    fun flush() {
        flushLogSpam(force = true)
        flushRuntimeCalls(force = true)
        writer?.flush()
    }

    @JvmStatic
    fun enterMethod(ownerName: String?): Long {
        if (writer == null) return 0L
        val normalizedOwner = normalizedContextValue(ownerName) ?: return 0L
        val now = nowMs()
        val stack = runtimeCallStack.get() ?: ArrayList<RuntimeCallFrame>(16).also(runtimeCallStack::set)
        stack.add(RuntimeCallFrame(normalizedOwner, now))
        return now
    }

    @JvmStatic
    fun exitMethod(startMs: Long, ownerName: String?) {
        if (startMs <= 0L || writer == null) return
        val normalizedOwner = normalizedContextValue(ownerName) ?: return
        val stack = runtimeCallStack.get() ?: return
        if (stack.isEmpty()) {
            runtimeCallStack.remove()
            return
        }
        val frame = popRuntimeCallFrame(stack, normalizedOwner, startMs)
        val caller = stack.lastOrNull()?.owner
        val durationMs = maxOf(0L, nowMs() - frame.startMs)
        if (caller != null && caller != frame.owner) {
            recordRuntimeCallEdge(caller, frame.owner, durationMs)
        }
        if (stack.isEmpty()) {
            runtimeCallStack.remove()
        }
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
        val attributedOwner = firstContextValue(owner, this.owner.get())
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
            flags,
        )
        val failed = flags and io.jankhunter.runtime.internal.io.BinaryLogWriter.FLAG_HTTP_FAILED != 0L || statusClass >= 5
        if (failed || durationMs >= SLOW_HTTP_THRESHOLD_MS) {
            recordProblemWindow("http_slow_or_failed", durationMs, 1, durationMs, attributedOwner)
        }
    }

    @JvmStatic
    fun recordStall(owner: String?, stackHint: String?, durationMs: Long) {
        val attributedOwner = firstContextValue(owner, this.owner.get())
        ensureContextRecorded(ownerOverride = attributedOwner)
        writer?.stall(attributedOwner, stackHint, durationMs)
        recordProblemWindow("main_thread_stall", durationMs, 1, durationMs, attributedOwner)
    }

    @JvmStatic
    fun recordMemory(pssKb: Long, javaHeapKb: Long, nativeHeapKb: Long) {
        if (!shouldRecordMemorySample(pssKb, javaHeapKb, nativeHeapKb)) {
            recordCounter("jankhunter.memory_sample.skipped.count", 1)
            return
        }
        ensureContextRecorded()
        writer?.memory(pssKb, javaHeapKb, nativeHeapKb)
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
        writer?.retained(tuple.screen, tuple.owner, tuple.flow, tuple.step, className, holder, ageMs, count)
        recordProblemWindow("retained_object", ageMs, count, ageMs, retainedOwner)
        maybeDumpRetainedHeap(className, retainedOwner, ageMs, count)
    }

    @JvmStatic
    fun watchObject(instance: Any?, description: String? = null) {
        watchObject(instance, description, null)
    }

    @JvmStatic
    fun watchObject(instance: Any?, description: String?, ownerHint: String?) {
        val retainedBy = firstContextValue(ownerHint, owner.get())
        objectRetentionWatcher?.watch(instance, description, retainedBy)
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
        val attributedScreen = firstContextValue(screen, this.screen)
        ensureContextRecorded(screenOverride = attributedScreen)
        writer?.uiWindow(attributedScreen, windowMs, frameCount, jankCount, p50Ms, p95Ms, p99Ms)
        if (jankCount > 0 || p95Ms >= UI_PROBLEM_FRAME_THRESHOLD_MS) {
            recordProblemWindow("ui_jank", windowMs, jankCount, p95Ms)
        }
    }

    @JvmStatic
    fun recordCounter(name: String?, value: Long) {
        val asyncWriter = writer ?: return
        if (shouldAggregateMetrics()) {
            metricAggregator.counter(name, value)
            flushMetrics(force = false)
            return
        }
        ensureContextRecorded()
        asyncWriter.counter(name, value)
    }

    @JvmStatic
    fun recordGauge(name: String?, value: Long) {
        val asyncWriter = writer ?: return
        if (shouldAggregateMetrics()) {
            metricAggregator.gauge(name, value)
            flushMetrics(force = false)
            return
        }
        ensureContextRecorded()
        asyncWriter.gauge(name, value)
    }

    @JvmStatic
    fun recordLogSpam(ownerName: String?, source: String?, level: Int) {
        val tuple = captureContext(ownerOverride = firstContextValue(ownerName, owner.get()))
        val key = LogSpamKey(tuple.screen, tuple.owner, tuple.flow, tuple.step, normalizedContextValue(source), level)
        val maxKeys = config?.maxLogSpamKeys() ?: DEFAULT_MAX_LOG_SPAM_KEYS
        if (!logSpamCounters.containsKey(key) && logSpamCounters.size >= maxKeys) {
            recordCounter("jankhunter.log_spam.dropped_keys.count", 1)
            flushLogSpam(force = false)
            return
        }
        logSpamCounters.computeIfAbsent(key) { AtomicLong() }.incrementAndGet()
        flushLogSpam(force = false)
    }

    internal fun captureContext(
        screenOverride: String? = null,
        ownerOverride: String? = null,
    ): JankHunterContext {
        return JankHunterContext(
            screen = normalizedContextValue(firstContextValue(screenOverride, screen)),
            owner = normalizedContextValue(firstContextValue(ownerOverride, owner.get())),
            flow = normalizedContextValue(flow.get()),
            step = normalizedContextValue(flowStep.get()),
        )
    }

    internal fun <T> callWithOwner(ownerName: String?, block: () -> T): T {
        return callWithContext(captureContext(), ownerName, block)
    }

    internal fun <T> callWithContext(context: JankHunterContext, ownerName: String?, block: () -> T): T {
        val previousOwner = owner.get()
        val previousFlow = flow.get()
        val previousStep = flowStep.get()
        val previousScreen = screen
        setThreadLocal(owner, normalizedContextValue(firstContextValue(ownerName, context.owner)))
        setThreadLocal(flow, context.flow)
        setThreadLocal(flowStep, context.step)
        context.screen?.let { screen = it }
        ensureContextRecorded()
        try {
            return block()
        } finally {
            setThreadLocal(owner, previousOwner)
            setThreadLocal(flow, previousFlow)
            setThreadLocal(flowStep, previousStep)
            screen = previousScreen
            ensureContextRecorded()
        }
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
        val tuple = captureContext(ownerOverride = firstContextValue(ownerOverride, owner.get()))
        writer?.problemWindow(tuple.screen, tuple.owner, tuple.flow, tuple.step, kind, windowMs, count, maxMs)
    }

    private fun shouldRecordMemorySample(pssKb: Long, javaHeapKb: Long, nativeHeapKb: Long): Boolean {
        val localConfig = config
        if (localConfig?.adaptiveSamplingEnabled() != true) return true
        return adaptiveRuntimeSampler?.shouldRecordMemory(nowMs(), pssKb, javaHeapKb, nativeHeapKb) ?: true
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
        val localConfig = config
        if (localConfig?.adaptiveSamplingEnabled() != true) return true
        return adaptiveRuntimeSampler?.shouldRecordContext(
            nowMs(),
            networkKind,
            batteryPct,
            availMemoryKb,
            lowMemory,
            networkMetered,
            networkValidated,
            rxBytes,
            txBytes,
            networkVpn,
        ) ?: true
    }

    private fun shouldAggregateMetrics(): Boolean {
        val localConfig = config ?: return false
        return localConfig.metricAggregationEnabled() && localConfig.maxMetricAggregationKeys() > 0
    }

    private fun flushMetrics(force: Boolean) {
        val asyncWriter = writer ?: return
        val localConfig = config ?: return
        if (!localConfig.metricAggregationEnabled()) return
        val now = nowMs()
        val last = lastMetricFlushAtMs.get()
        val interval = localConfig.metricAggregationWindowMs()
        if (!force && (interval <= 0 || now - last < interval)) return
        if (!lastMetricFlushAtMs.compareAndSet(last, now) && !force) return
        ensureContextRecorded()
        metricAggregator.flush(object : MetricAggregator.Sink {
            override fun counter(name: String, value: Long) {
                asyncWriter.counter(name, value)
            }

            override fun gauge(name: String, value: Long) {
                asyncWriter.gauge(name, value)
            }
        })
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
        val key = tuple.key()
        synchronized(contextLock) {
            if (key == lastContextKey) return
            lastContextKey = key
            asyncWriter.flowContext(tuple.screen, tuple.owner, tuple.flow, tuple.step)
        }
    }

    private fun flushLogSpam(force: Boolean) {
        val asyncWriter = writer ?: return
        val now = nowMs()
        val last = lastLogSpamFlushAtMs.get()
        if (!force && now - last < LOG_SPAM_FLUSH_MS) return
        if (!lastLogSpamFlushAtMs.compareAndSet(last, now) && !force) return

        logSpamCounters.forEach { (key, counter) ->
            val count = counter.getAndSet(0)
            if (count <= 0) return@forEach
            asyncWriter.logSpam(key.screen, key.owner, key.flow, key.step, key.source, key.level, count)
            if (count >= LOG_SPAM_PROBLEM_COUNT) {
                asyncWriter.problemWindow(
                    key.screen,
                    key.owner,
                    key.flow,
                    key.step,
                    "log_spam",
                    LOG_SPAM_FLUSH_MS,
                    count,
                    count,
                )
            }
        }
        logSpamCounters.forEach { (key, counter) ->
            if (counter.get() == 0L) {
                logSpamCounters.remove(key, counter)
            }
        }
    }

    private fun recordRuntimeCallEdge(caller: String, callee: String, durationMs: Long) {
        val tuple = captureContext(ownerOverride = caller)
        val key = RuntimeCallKey(tuple.screen, caller, tuple.flow, tuple.step, callee)
        val stats = runtimeCallCounters[key] ?: run {
            val maxKeys = config?.maxRuntimeCallGraphKeys() ?: DEFAULT_MAX_RUNTIME_CALL_GRAPH_KEYS
            if (maxKeys <= 0 || runtimeCallCounters.size >= maxKeys) {
                runtimeCallDropped.incrementAndGet()
                flushRuntimeCalls(force = false)
                return
            }
            runtimeCallCounters.computeIfAbsent(key) { RuntimeCallStats() }
        }
        stats.count.incrementAndGet()
        stats.totalMs.addAndGet(durationMs)
        stats.updateMax(durationMs)
        flushRuntimeCalls(force = false)
    }

    private fun flushRuntimeCalls(force: Boolean) {
        val asyncWriter = writer ?: return
        val now = nowMs()
        val last = lastRuntimeCallFlushAtMs.get()
        if (!force && now - last < RUNTIME_CALL_FLUSH_MS) return
        if (!lastRuntimeCallFlushAtMs.compareAndSet(last, now) && !force) return

        runtimeCallCounters.forEach { (key, stats) ->
            val count = stats.count.getAndSet(0)
            if (count <= 0) return@forEach
            val totalMs = stats.totalMs.getAndSet(0)
            val maxMs = stats.maxMs.getAndSet(0)
            asyncWriter.runtimeCall(key.screen, key.caller, key.flow, key.step, key.callee, count, totalMs, maxMs)
        }
        runtimeCallCounters.forEach { (key, stats) ->
            if (stats.count.get() == 0L) {
                runtimeCallCounters.remove(key, stats)
            }
        }
        val dropped = runtimeCallDropped.getAndSet(0)
        if (dropped > 0) {
            asyncWriter.counter("jankhunter.runtime_call_graph.dropped.count", dropped)
        }
    }

    private fun popRuntimeCallFrame(
        stack: MutableList<RuntimeCallFrame>,
        ownerName: String,
        fallbackStartMs: Long,
    ): RuntimeCallFrame {
        val lastIndex = stack.lastIndex
        val last = stack[lastIndex]
        if (last.owner == ownerName) {
            stack.removeAt(lastIndex)
            return last
        }
        for (index in lastIndex - 1 downTo 0) {
            if (stack[index].owner == ownerName) {
                return stack.removeAt(index)
            }
        }
        return RuntimeCallFrame(ownerName, fallbackStartMs)
    }

    private fun <T> setThreadLocal(target: ThreadLocal<T>, value: T?) {
        if (value == null) {
            target.remove()
        } else {
            target.set(value)
        }
    }

    private fun nowMs(): Long = SystemClock.elapsedRealtime()

    private fun firstContextValue(primary: String?, fallback: String?): String? {
        return normalizedContextValue(primary) ?: normalizedContextValue(fallback)
    }

    private fun normalizedContextValue(value: String?): String? {
        val normalized = value?.trim()?.takeIf { it.isNotEmpty() }
        return normalized?.takeUnless { it == "unknown" }
    }

    private fun metricOwner(ownerName: String?): String {
        return ownerName
            ?.takeIf { it.isNotBlank() }
            ?.replace(Regex("\\s+"), "_")
            ?: "unknown"
    }

    private fun hasAdditionalTypeContract(value: Any, plainType: Class<*>): Boolean {
        val valueType = value.javaClass
        if (valueType.interfaces.any { it != plainType }) return true

        var current = valueType.superclass
        while (current != null && current != Any::class.java) {
            if (plainType.isAssignableFrom(current)) return true
            current = current.superclass
        }

        return false
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

    private data class HandlerRunnableEntry(
        val handler: WeakReference<Handler>,
        val original: WeakReference<Runnable>,
        val wrappers: MutableList<HandlerRunnableWrapperEntry>,
    )

    private data class HandlerRunnableWrapperEntry(
        val wrapper: WeakReference<Runnable>,
        val token: WeakReference<Any>?,
    )

    private const val WRAPPED_WORK_GAUGE_THRESHOLD_MS = 50L
    private const val WRAPPED_WORK_PROBLEM_THRESHOLD_MS = 250L
    private const val SLOW_HTTP_THRESHOLD_MS = 1000L
    private const val UI_PROBLEM_FRAME_THRESHOLD_MS = 32L
    private const val LOG_SPAM_FLUSH_MS = 5000L
    private const val LOG_SPAM_PROBLEM_COUNT = 50L
    private const val DEFAULT_MAX_METRIC_AGGREGATION_KEYS = 2048
    private const val DEFAULT_MAX_LOG_SPAM_KEYS = 2048
    private const val DEFAULT_MAX_RUNTIME_CALL_GRAPH_KEYS = 4096
    private const val DEFAULT_MAX_HANDLER_TRACKING_ENTRIES = 4096
    private const val DEFAULT_MAX_HANDLER_WRAPPERS_PER_RUNNABLE = 32

    internal data class JankHunterContext(
        val screen: String?,
        val owner: String?,
        val flow: String?,
        val step: String?,
    ) {
        fun key(): String = listOf(screen.orEmpty(), owner.orEmpty(), flow.orEmpty(), step.orEmpty()).joinToString("\u0001")
    }

    private data class LogSpamKey(
        val screen: String?,
        val owner: String?,
        val flow: String?,
        val step: String?,
        val source: String?,
        val level: Int,
    )

    private data class RuntimeCallFrame(
        val owner: String,
        val startMs: Long,
    )

    private data class RuntimeCallKey(
        val screen: String?,
        val caller: String,
        val flow: String?,
        val step: String?,
        val callee: String,
    )

    private class RuntimeCallStats {
        val count = AtomicLong()
        val totalMs = AtomicLong()
        val maxMs = AtomicLong()

        fun updateMax(value: Long) {
            while (true) {
                val current = maxMs.get()
                if (value <= current) return
                if (maxMs.compareAndSet(current, value)) return
            }
        }
    }

    private const val RUNTIME_CALL_FLUSH_MS = 5000L
}
