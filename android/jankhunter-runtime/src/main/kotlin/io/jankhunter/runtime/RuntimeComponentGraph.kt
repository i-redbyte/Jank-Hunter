package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.util.concurrent.TimeUnit

/**
 * Process-local composition root. It owns construction and wiring only; behavior remains in the
 * cohesive runtime services exposed below.
 */
internal class RuntimeComponentGraph(
    nowMs: RuntimeLongSource,
    nowUs: RuntimeLongSource,
) {
    val state = RuntimeState()
    val contextTracker = ContextTracker()
    val coordinator = RuntimeCoordinator(state, nowMs)
    val telemetryAccess = RuntimeTelemetryAccess(
        state,
        contextTracker,
        coordinator,
        nowMs,
        AndroidProcessImportanceSource(),
    )
    val operationTelemetry = RuntimeOperationTelemetry(
        contextTracker,
        { writer },
        nowUs,
        telemetryAccess::ensureContextRecorded,
    )
    val metrics = RuntimeMetricsService(
        DEFAULT_MAX_METRIC_AGGREGATION_KEYS,
        nowMs,
        { writer },
        { config },
        telemetryAccess::ensureContextRecorded,
        { task -> state.maintenanceScheduler?.execute(task) == true },
        { delayMs, task -> state.maintenanceScheduler?.executeDelayed(delayMs, task) == true },
        { timeoutMs, task -> state.maintenanceScheduler?.executeAndWait(timeoutMs, task) == true },
    )
    val sampling = RuntimeSamplingService(nowMs)
    val runtimeHookEvents = RuntimeHookEventTransport(
        maxCounterKeys = { config?.maxRuntimeCallGraphKeys() ?: DEFAULT_MAX_RUNTIME_CALL_GRAPH_KEYS },
        maxLogSpamKeys = { config?.maxLogSpamKeys() ?: DEFAULT_MAX_LOG_SPAM_KEYS },
        exactAdmission = { config?.exactEventCollectionEnabled() != false },
    )
    val runtimeCallGraph = RuntimeCallGraph(
        nowMs = nowMs,
        captureScreen = contextTracker::currentScreenOrNull,
        captureOperationId = contextTracker::currentOperationId,
        maxKeys = { config?.maxRuntimeCallGraphKeys() ?: DEFAULT_MAX_RUNTIME_CALL_GRAPH_KEYS },
        exactAdmission = { config?.exactEventCollectionEnabled() != false },
        admissionWaitNanos = {
            val activeConfig = config
            val waitMs = if (Thread.currentThread().name == "main") {
                activeConfig?.mainThreadAdmissionWaitMs() ?: 0L
            } else {
                activeConfig?.backgroundAdmissionWaitMs() ?: 5L
            }
            TimeUnit.MILLISECONDS.toNanos(waitMs)
        },
    )
    val semanticTelemetry = RuntimeSemanticTelemetry(telemetryAccess, runtimeCallGraph)
    val workerTelemetry = RuntimeWorkerTelemetry(telemetryAccess, semanticTelemetry)
    val httpTelemetry = RuntimeHttpTelemetry(telemetryAccess)
    val webSocketTelemetry = RuntimeWebSocketTelemetry(telemetryAccess)
    val databaseTelemetry = RuntimeDatabaseTelemetry(telemetryAccess)
    val androidComponentTelemetry = RuntimeAndroidComponentTelemetry(telemetryAccess, nowUs)
    val binderTelemetry = RuntimeBinderTelemetry(telemetryAccess, nowUs)
    val manualDatabaseTracing = RuntimeManualDatabaseTracing(databaseTelemetry)
    val ioTelemetry = RuntimeIOTelemetry(telemetryAccess)
    val asyncTelemetry = RuntimeAsyncTelemetry(telemetryAccess, metrics, operationTelemetry)
    val handlerHooks = RuntimeHandlerHooks(telemetryAccess, asyncTelemetry)
    val retentionTelemetry = RuntimeRetentionTelemetry(state, telemetryAccess, metrics, nowMs)
    val contextTelemetry = RuntimeContextTelemetry(
        state,
        contextTracker,
        telemetryAccess,
        runtimeCallGraph,
        nowMs,
    )
    val systemTelemetry = RuntimeSystemTelemetry(
        contextTracker,
        telemetryAccess,
        metrics,
        sampling,
        runtimeHookEvents,
    )
    val collectorTelemetry: RuntimeCollectorTelemetry = RuntimeCollectorTelemetry(
        contextTracker,
        telemetryAccess,
        contextTelemetry,
        operationTelemetry,
        retentionTelemetry,
        systemTelemetry,
        asyncTelemetry,
        { session.requestFlush() },
    )
    val collectors: RuntimeCollectorService = RuntimeCollectorService(
        state,
        collectorTelemetry,
        retentionTelemetry::recordWatchedRetained,
        retentionTelemetry::dumpWatchedRetainedHeap,
    )
    val storageValve = RuntimeStorageValve(state, collectors)
    private val writerFactory = AsyncLogWriterFactory()
    val session: RuntimeSessionController = RuntimeSessionController(
        state,
        coordinator,
        metrics,
        sampling,
        runtimeHookEvents,
        runtimeCallGraph,
        handlerHooks,
        collectors,
        writerFactory,
        nowMs,
    )
    val lifecycle = RuntimeLifecycleController(state, coordinator, session, metrics, nowMs)
    val networkAdapterTelemetry = RuntimeNetworkAdapterTelemetry(
        telemetryAccess,
        contextTelemetry,
        httpTelemetry,
        webSocketTelemetry,
        systemTelemetry,
    )
    val manualTelemetry = RuntimeManualTelemetry(
        contextTracker,
        contextTelemetry,
        operationTelemetry,
        retentionTelemetry,
        systemTelemetry,
        ioTelemetry,
        semanticTelemetry,
    )
    val instrumentationHooks = RuntimeInstrumentationHooks(
        telemetryAccess,
        runtimeCallGraph,
        runtimeHookEvents,
        semanticTelemetry,
        workerTelemetry,
        databaseTelemetry,
        androidComponentTelemetry,
        binderTelemetry,
        systemTelemetry,
        asyncTelemetry,
        handlerHooks,
        contextTelemetry,
        operationTelemetry,
        retentionTelemetry,
        { config },
    )

    val writer: AsyncLogWriter?
        get() = telemetryAccess.writer

    val config: JankHunterConfig?
        get() = telemetryAccess.config

    private companion object {
        const val DEFAULT_MAX_METRIC_AGGREGATION_KEYS = 2048
        const val DEFAULT_MAX_RUNTIME_CALL_GRAPH_KEYS = 4096
        const val DEFAULT_MAX_LOG_SPAM_KEYS = 2048
    }
}
