package io.jankhunter.artti.internal

import android.annotation.SuppressLint
import android.content.Context
import android.content.pm.ApplicationInfo
import android.os.Build
import android.os.Debug
import android.os.SystemClock
import android.util.Log
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterAgentEventBatch
import io.jankhunter.runtime.JankHunterAgentEventSink
import io.jankhunter.runtime.JankHunterAgentEventType
import io.jankhunter.runtime.JankHunterContextSnapshot
import io.jankhunter.runtime.JankHunterRuntimeIntegration
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference
import java.util.concurrent.locks.LockSupport

class ArtTiIntegration : JankHunterRuntimeIntegration {
    override val id: String = "artti"

    private val lifecycle = AtomicReference(Lifecycle.UNINITIALIZED)
    private val loggedReasons = AtomicInteger()
    private val lastContextToken = ThreadLocal<Long>()
    private val statusBatch = JankHunterAgentEventBatch(1)

    @Volatile
    private var worker: Thread? = null

    @Volatile
    private var config: ArtTiRuntimeConfig? = null

    @Volatile
    private var triggerBudget: ArtTiTriggerBudget? = null

    @Volatile
    private var eventSink: JankHunterAgentEventSink? = null

    override fun start(context: Context, eventSink: JankHunterAgentEventSink) {
        while (true) {
            val observed = lifecycle.get()
            if (observed != Lifecycle.UNINITIALIZED && observed != Lifecycle.STOPPED) return
            if (lifecycle.compareAndSet(observed, Lifecycle.ATTACHING)) break
        }
        this.eventSink = eventSink
        report(Reason.ATTACH_REQUESTED)
        val appContext = context.applicationContext ?: context
        worker = Thread({ runControl(appContext) }, CONTROL_THREAD_NAME).apply {
            isDaemon = true
            priority = Thread.MIN_PRIORITY
            start()
        }
    }

    override fun stop(timeoutMs: Long) {
        val observed = lifecycle.getAndSet(Lifecycle.STOPPING)
        if (observed == Lifecycle.UNINITIALIZED || observed == Lifecycle.STOPPED) {
            lifecycle.set(Lifecycle.STOPPED)
            return
        }
        val current = worker
        current?.interrupt()
        if (current != null && current !== Thread.currentThread()) {
            try {
                current.join(timeoutMs.coerceIn(0L, MAX_STOP_WAIT_MS))
            } catch (_: InterruptedException) {
                Thread.currentThread().interrupt()
            }
        }
        if (current?.isAlive == true) {
            report(Reason.SHUTDOWN_TIMEOUT, warning = true)
        } else {
            lifecycle.set(Lifecycle.STOPPED)
            report(Reason.STOPPED)
        }
    }

    override fun onContextChanged(
        thread: Thread,
        screen: String?,
        owner: String?,
        flow: String?,
        step: String?,
    ) {
        if (!isActive()) return
        val token = contextToken(screen, owner, flow, step)
        if (lastContextToken.get() == token) return
        lastContextToken.set(token)
        if (eventSink?.tryPublishContext(token, screen, owner, flow, step) != true) {
            JankHunter.recordCounter("jankhunter.artti.context_sink_drop.count", 1L)
        }
        val result = ArtTiNativeBridge.nativeLinkThreadContext(thread, token)
        if (result < 0) report(Reason.CORRELATION_DROP)
    }

    override fun onMainThreadStall(thread: Thread, context: JankHunterContextSnapshot) {
        val runtimeConfig = config ?: return
        if (!isActive() || !runtimeConfig.triggerPolicy.onMainThreadStall) return
        if (triggerBudget?.tryAcquire(SystemClock.elapsedRealtime()) != true) {
            report(Reason.STACK_BUDGET_DROP)
            return
        }
        val token = contextToken(context.screen, context.owner, context.flow, context.step)
        val result = ArtTiNativeBridge.nativeCaptureStack(
            thread = thread,
            trigger = STACK_TRIGGER_MAIN_THREAD_STALL,
            contextToken = token,
            relatedSequence = 0L,
        )
        if (result < 0) report(Reason.STACK_CAPTURE_FAILED)
    }

    @SuppressLint("NewApi")
    private fun runControl(context: Context) {
        var nativeStarted = false
        var phase = ControlPhase.PRECONDITIONS
        try {
            if (Build.VERSION.SDK_INT < MIN_ATTACH_API) {
                lifecycle.set(Lifecycle.DISABLED)
                report(Reason.API_UNSUPPORTED)
                return
            }
            if (context.applicationInfo.flags and ApplicationInfo.FLAG_DEBUGGABLE == 0) {
                lifecycle.set(Lifecycle.DISABLED)
                report(Reason.APP_NOT_DEBUGGABLE)
                return
            }
            phase = ControlPhase.CONFIG
            val runtimeConfig = ArtTiRuntimeConfigParser.fromManifest(context).getOrElse {
                lifecycle.set(Lifecycle.FAILED)
                report(Reason.CONFIG_INVALID, warning = true)
                return
            }
            config = runtimeConfig
            triggerBudget = ArtTiTriggerBudget(
                runtimeConfig.triggerPolicy.minTriggerIntervalMs,
                runtimeConfig.triggerPolicy.maxSamplesPerMinute,
            )
            phase = ControlPhase.LIBRARY_LOAD
            if (ArtTiNativeBridge.loadLibrary().isFailure) {
                lifecycle.set(Lifecycle.DISABLED)
                report(Reason.LIBRARY_OR_ABI_MISSING, warning = true)
                return
            }
            phase = ControlPhase.NATIVE_INITIALIZE
            val initializeStatus = ArtTiNativeStatus.fromWire(
                ArtTiNativeBridge.nativeInitialize(runtimeConfig.native.encodeDirect()),
            )
            if (initializeStatus != ArtTiNativeStatus.OK) {
                lifecycle.set(Lifecycle.FAILED)
                report(Reason.NATIVE_INITIALIZE_FAILED, warning = true)
                return
            }
            nativeStarted = true
            phase = ControlPhase.PRE_ATTACH_HANDSHAKE
            verifyHandshake(runtimeConfig)
            phase = ControlPhase.ATTACH
            // An absolute package install path may contain '=' in its base64 directory segment.
            // Debug.attachJvmtiAgent rejects '=' in the library argument, so delegate lookup to
            // the application class loader as intended by the public API.
            Debug.attachJvmtiAgent(NATIVE_LIBRARY_FILE, runtimeConfig.agentOptions, context.classLoader)
            phase = ControlPhase.POST_ATTACH_HANDSHAKE
            val handshake = verifyHandshake(runtimeConfig)
            phase = ControlPhase.ACTIVATE
            if (!lifecycle.compareAndSet(Lifecycle.ATTACHING, Lifecycle.ACTIVE)) return
            publishRuntimeDescriptor(runtimeConfig, handshake)
            report(Reason.ATTACH_SUCCEEDED)
            phase = ControlPhase.METADATA_REFRESH
            val refreshed = ArtTiNativeBridge.nativeRefreshThreadMetadata()
            if (refreshed < 0) report(Reason.THREAD_METADATA_DEGRADED)
            phase = ControlPhase.DRAIN
            drainLoop(runtimeConfig)
        } catch (failure: Throwable) {
            if (lifecycle.get() != Lifecycle.STOPPING) {
                lifecycle.set(Lifecycle.FAILED)
                report(phase.failureReason, warning = true, safeDetail = failure.safeClassName())
            }
        } finally {
            if (nativeStarted) {
                config?.let { runtimeConfig ->
                    runCatching { drainRemaining(runtimeConfig, resolveMethods = true) }
                        .onFailure { report(Reason.FINAL_DRAIN_FAILED) }
                }
                val stopStatus = runCatching {
                    ArtTiNativeStatus.fromWire(ArtTiNativeBridge.nativeStop())
                }.getOrElse {
                    report(
                        Reason.NATIVE_SHUTDOWN_INCOMPLETE,
                        warning = true,
                        safeDetail = it.safeClassName(),
                    )
                    ArtTiNativeStatus.INTERNAL
                }
                if (stopStatus != ArtTiNativeStatus.OK) {
                    report(Reason.NATIVE_SHUTDOWN_INCOMPLETE, warning = true)
                }
                config?.let { runtimeConfig ->
                    runCatching { drainRemaining(runtimeConfig, resolveMethods = false) }
                        .onFailure { report(Reason.FINAL_DRAIN_FAILED) }
                }
            }
            worker = null
            if (lifecycle.get() == Lifecycle.STOPPING) lifecycle.set(Lifecycle.STOPPED)
        }
    }

    private fun verifyHandshake(runtimeConfig: ArtTiRuntimeConfig): ArtTiNativeHandshake {
        val buffer = ArtTiNativeProtocol.allocateHandshakeBuffer()
        val status = ArtTiNativeStatus.fromWire(ArtTiNativeBridge.nativeHandshake(buffer))
        require(status == ArtTiNativeStatus.OK) { "native_handshake_failed" }
        val handshake = ArtTiNativeHandshake.decode(buffer).getOrThrow()
        require(handshake.isCompatible()) { "native_handshake_incompatible" }
        require(handshake.configHash == runtimeConfig.native.configHash) { "native_config_hash_mismatch" }
        return handshake
    }

    private fun drainLoop(runtimeConfig: ArtTiRuntimeConfig) {
        val output = ArtTiNativeProtocol.allocateDrainBuffer(runtimeConfig.native.drainBatchSize)
        val decoder = ArtTiNativeProtocolDecoder()
        val contexts = ArtTiThreadContextTable(runtimeConfig.native.maxTrackedThreads)
        val semanticBatch = JankHunterAgentEventBatch(runtimeConfig.native.drainBatchSize)
        val methodBuffer = ArtTiMethodDefinition.allocateBuffer()
        val visitor = ArtTiNativeRecordVisitor { record ->
            onNativeRecord(record, runtimeConfig, contexts, semanticBatch, methodBuffer)
        }
        var consecutiveErrors = 0
        while (isActive()) {
            semanticBatch.clear()
            val bytesWritten = ArtTiNativeBridge.nativeDrain(output, runtimeConfig.native.drainBatchSize)
            if (bytesWritten < 0) {
                consecutiveErrors++
                report(Reason.DRAIN_FAILED)
                if (consecutiveErrors >= MAX_CONSECUTIVE_DRAIN_ERRORS) error("native_drain_failed")
            } else {
                val result = decoder.decode(output, bytesWritten, visitor)
                if (result.status == ArtTiNativeStatus.OK) {
                    consecutiveErrors = 0
                    if (semanticBatch.size > 0 && eventSink?.tryPublish(semanticBatch) != true) {
                        report(Reason.EVENT_SINK_DROP)
                    }
                } else {
                    consecutiveErrors++
                    report(Reason.DECODE_FAILED)
                    if (consecutiveErrors >= MAX_CONSECUTIVE_DRAIN_ERRORS) error("native_decode_failed")
                }
            }
            LockSupport.parkNanos(runtimeConfig.triggerPolicy.drainIntervalMs * NANOS_PER_MS)
            if (Thread.interrupted() && !isActive()) return
        }
    }

    private fun drainRemaining(runtimeConfig: ArtTiRuntimeConfig, resolveMethods: Boolean) {
        val output = ArtTiNativeProtocol.allocateDrainBuffer(runtimeConfig.native.drainBatchSize)
        val decoder = ArtTiNativeProtocolDecoder()
        val semanticBatch = JankHunterAgentEventBatch(runtimeConfig.native.drainBatchSize)
        val methodBuffer = ArtTiMethodDefinition.allocateBuffer()
        val visitor = ArtTiNativeRecordVisitor { record ->
            if (resolveMethods && record.type == EVENT_STACK_DEFINITION) {
                resolveMethodDefinition(record.payload1, methodBuffer)
            }
            if (!appendCanonical(record, semanticBatch)) report(Reason.CANONICAL_BATCH_DROP)
        }
        repeat(MAX_FINAL_DRAIN_BATCHES) {
            semanticBatch.clear()
            val bytesWritten = ArtTiNativeBridge.nativeDrain(output, runtimeConfig.native.drainBatchSize)
            if (bytesWritten < 0) {
                report(Reason.FINAL_DRAIN_FAILED)
                return
            }
            val result = decoder.decode(output, bytesWritten, visitor)
            if (result.status != ArtTiNativeStatus.OK) {
                report(Reason.FINAL_DRAIN_FAILED)
                return
            }
            if (semanticBatch.size > 0 && eventSink?.tryPublish(semanticBatch) != true) {
                report(Reason.EVENT_SINK_DROP)
            }
            if (result.recordsSeen == 0) return
        }
        report(Reason.FINAL_DRAIN_TRUNCATED)
    }

    private fun onNativeRecord(
        record: ArtTiNativeRecordView,
        runtimeConfig: ArtTiRuntimeConfig,
        contexts: ArtTiThreadContextTable,
        semanticBatch: JankHunterAgentEventBatch,
        methodBuffer: java.nio.ByteBuffer,
    ) {
        when (record.type) {
            EVENT_AGENT_STATUS -> JankHunter.recordGauge("jankhunter.artti.agent_status", record.payload0)
            EVENT_CAPABILITY -> {
                JankHunter.recordGauge("jankhunter.artti.capabilities.requested", record.payload0)
                JankHunter.recordGauge("jankhunter.artti.capabilities.potential", record.payload1)
                JankHunter.recordGauge("jankhunter.artti.capabilities.granted", record.payload2)
                JankHunter.recordGauge("jankhunter.artti.capabilities.active", record.payload3)
                if (record.payload3 and record.payload0 != record.payload0) {
                    lifecycle.compareAndSet(Lifecycle.ACTIVE, Lifecycle.DEGRADED)
                    report(Reason.CAPABILITY_DEGRADED)
                }
            }
            EVENT_QUALITY -> {
                JankHunter.recordGauge("jankhunter.artti.queue.high_watermark", record.payload0)
                JankHunter.recordGauge("jankhunter.artti.queue.full_total", record.payload1)
                JankHunter.recordGauge("jankhunter.artti.queue.contention_total", record.payload2)
                JankHunter.recordGauge("jankhunter.artti.native_other_loss_total", record.payload3)
            }
            EVENT_THREAD_END -> contexts.remove(record.threadToken)
            EVENT_MONITOR_CONTENTION -> maybeCaptureLongContention(record, runtimeConfig, contexts)
            EVENT_STACK_DEFINITION -> resolveMethodDefinition(record.payload1, methodBuffer)
            EVENT_CORRELATION_LINK -> if (!contexts.put(record.threadToken, record.contextToken)) {
                report(Reason.CONTEXT_TABLE_DROP)
            }
        }
        if (!appendCanonical(record, semanticBatch)) {
            report(Reason.CANONICAL_BATCH_DROP)
        }
    }

    private fun appendCanonical(
        record: ArtTiNativeRecordView,
        semanticBatch: JankHunterAgentEventBatch,
    ): Boolean = semanticBatch.tryAppend(
        type = record.type,
        schemaVersion = record.schemaVersion,
        flags = record.flags,
        producerSequence = record.producerSequence,
        monotonicNs = record.monotonicNs,
        producerId = record.producerId,
        threadToken = record.threadToken,
        contextToken = record.contextToken,
        payload0 = record.payload0,
        payload1 = record.payload1,
        payload2 = record.payload2,
        payload3 = record.payload3,
    )

    private fun resolveMethodDefinition(methodId: Long, output: java.nio.ByteBuffer) {
        if (methodId == 0L) return
        val bytesWritten = ArtTiNativeBridge.nativeResolveMethod(methodId, output)
        if (bytesWritten == 0) return
        if (bytesWritten < 0) {
            report(Reason.METHOD_RESOLUTION_DROP)
            return
        }
        val definition = ArtTiMethodDefinition.decode(output, bytesWritten).getOrElse {
            report(Reason.METHOD_RESOLUTION_DROP)
            return
        }
        val symbol = buildString {
            append(definition.classSignature)
            append("->")
            append(definition.methodName)
            append(definition.methodSignature)
        }.take(MAX_METHOD_SYMBOL_LENGTH)
        if (eventSink?.tryPublishMethodDefinition(definition.methodId, symbol) != true) {
            report(Reason.EVENT_SINK_DROP)
        }
    }

    private fun publishRuntimeDescriptor(
        runtimeConfig: ArtTiRuntimeConfig,
        handshake: ArtTiNativeHandshake,
    ) {
        publishCanonicalStatus(
            status = SDK_STATUS_CONFIG_APPLIED,
            flags = runtimeConfig.native.transportCapacity,
            profile = runtimeConfig.native.profile.toLong(),
            configHash = runtimeConfig.native.configHash,
            nativeMemoryBytes = handshake.nativeMemoryBytes,
        )
        val beforeNs = SystemClock.elapsedRealtimeNanos()
        val nativeNs = ArtTiNativeBridge.nativeMonotonicTimeNs()
        val afterNs = SystemClock.elapsedRealtimeNanos()
        val midpointNs = beforeNs + (afterNs - beforeNs) / 2L
        val uncertaintyNs = (afterNs - beforeNs).coerceAtLeast(0L) / 2L
        synchronized(statusBatch) {
            statusBatch.clear()
            statusBatch.tryAppend(
                type = JankHunterAgentEventType.CLOCK_SYNC,
                schemaVersion = 1,
                flags = 0,
                producerSequence = 0L,
                monotonicNs = nativeNs.coerceAtLeast(0L),
                producerId = 0L,
                threadToken = 0L,
                contextToken = 0L,
                payload0 = midpointNs.coerceAtLeast(0L),
                payload1 = uncertaintyNs,
                payload2 = 0L,
                payload3 = 0L,
            )
            if (eventSink?.tryPublish(statusBatch) != true) {
                JankHunter.recordCounter("jankhunter.artti.event_sink_drop.count", 1L)
            }
        }
    }

    private fun maybeCaptureLongContention(
        record: ArtTiNativeRecordView,
        runtimeConfig: ArtTiRuntimeConfig,
        contexts: ArtTiThreadContextTable,
    ) {
        if (!runtimeConfig.triggerPolicy.onLongContention) return
        if (triggerBudget?.tryAcquire(SystemClock.elapsedRealtime()) != true) {
            report(Reason.STACK_BUDGET_DROP)
            return
        }
        val result = ArtTiNativeBridge.nativeCaptureStackForToken(
            threadToken = record.threadToken,
            trigger = STACK_TRIGGER_LONG_CONTENTION,
            contextToken = contexts.get(record.threadToken),
            relatedSequence = record.producerSequence,
        )
        if (result < 0) report(Reason.STACK_CAPTURE_FAILED)
    }

    private fun isActive(): Boolean {
        val current = lifecycle.get()
        return current == Lifecycle.ACTIVE || current == Lifecycle.DEGRADED
    }

    private fun report(reason: Reason, warning: Boolean = false, safeDetail: String? = null) {
        JankHunter.recordCounter("jankhunter.artti.${reason.metric}.count", 1L)
        if (reason.loggable) publishCanonicalStatus(SDK_STATUS_REASON_BASE + reason.ordinal)
        if (!reason.loggable) return
        val mask = 1 shl reason.ordinal
        while (true) {
            val observed = loggedReasons.get()
            if (observed and mask != 0) return
            if (loggedReasons.compareAndSet(observed, observed or mask)) break
        }
        val message = buildString {
            append("ART TI agent: ")
            append(reason.metric)
            if (!safeDetail.isNullOrEmpty()) {
                append(" type=")
                append(safeDetail)
            }
        }
        if (warning) Log.w(TAG, message) else Log.i(TAG, message)
    }

    private fun publishCanonicalStatus(
        status: Int,
        flags: Int = 0,
        profile: Long = config?.native?.profile?.toLong() ?: 0L,
        configHash: Long = config?.native?.configHash ?: 0L,
        nativeMemoryBytes: Long = 0L,
    ) {
        val sink = eventSink ?: return
        synchronized(statusBatch) {
            statusBatch.clear()
            statusBatch.tryAppend(
                type = JankHunterAgentEventType.AGENT_STATUS,
                schemaVersion = 1,
                flags = flags,
                producerSequence = 0L,
                monotonicNs = SystemClock.elapsedRealtimeNanos().coerceAtLeast(0L),
                producerId = 0L,
                threadToken = 0L,
                contextToken = 0L,
                payload0 = status.toLong(),
                payload1 = profile,
                payload2 = configHash,
                payload3 = nativeMemoryBytes,
            )
            if (!sink.tryPublish(statusBatch)) {
                JankHunter.recordCounter("jankhunter.artti.event_sink_drop.count", 1L)
            }
        }
    }

    private fun Throwable.safeClassName(): String = javaClass.simpleName
        .filter { it.isLetterOrDigit() || it == '_' }
        .take(MAX_FAILURE_CLASS_LENGTH)

    private fun contextToken(screen: String?, owner: String?, flow: String?, step: String?): Long {
        var hash = FNV_OFFSET_BASIS
        fun add(value: String?) {
            if (value == null) {
                hash = (hash xor NULL_MARKER) * FNV_PRIME
            } else {
                value.forEach { character ->
                    hash = (hash xor character.code.toLong()) * FNV_PRIME
                }
            }
            hash = (hash xor FIELD_SEPARATOR) * FNV_PRIME
        }
        add(screen)
        add(owner)
        add(flow)
        add(step)
        return if (hash == 0L) 1L else hash
    }

    private enum class Lifecycle { UNINITIALIZED, ATTACHING, ACTIVE, DEGRADED, DISABLED, FAILED, STOPPING, STOPPED }

    private enum class ControlPhase(val failureReason: Reason) {
        PRECONDITIONS(Reason.PRECONDITION_FAILED),
        CONFIG(Reason.CONFIG_RUNTIME_FAILED),
        LIBRARY_LOAD(Reason.LIBRARY_OR_ABI_MISSING),
        NATIVE_INITIALIZE(Reason.NATIVE_BRIDGE_FAILED),
        PRE_ATTACH_HANDSHAKE(Reason.NATIVE_HANDSHAKE_FAILED),
        ATTACH(Reason.ATTACH_FAILED),
        POST_ATTACH_HANDSHAKE(Reason.NATIVE_HANDSHAKE_FAILED),
        ACTIVATE(Reason.ACTIVATION_FAILED),
        METADATA_REFRESH(Reason.METADATA_REFRESH_FAILED),
        DRAIN(Reason.DRAIN_TERMINATED),
    }

    private enum class Reason(val metric: String, val loggable: Boolean = false) {
        ATTACH_REQUESTED("attach_requested"),
        API_UNSUPPORTED("api_unsupported", true),
        APP_NOT_DEBUGGABLE("app_not_debuggable", true),
        CONFIG_INVALID("config_invalid", true),
        LIBRARY_OR_ABI_MISSING("library_or_abi_missing", true),
        NATIVE_INITIALIZE_FAILED("native_initialize_failed", true),
        ATTACH_SUCCEEDED("attach_succeeded", true),
        PRECONDITION_FAILED("precondition_failed", true),
        CONFIG_RUNTIME_FAILED("config_runtime_failed", true),
        NATIVE_BRIDGE_FAILED("native_bridge_failed", true),
        NATIVE_HANDSHAKE_FAILED("native_handshake_failed", true),
        ATTACH_FAILED("attach_failed", true),
        ACTIVATION_FAILED("activation_failed", true),
        METADATA_REFRESH_FAILED("metadata_refresh_failed", true),
        DRAIN_TERMINATED("drain_terminated", true),
        CAPABILITY_DEGRADED("capability_degraded", true),
        THREAD_METADATA_DEGRADED("thread_metadata_degraded"),
        DRAIN_FAILED("drain_failed"),
        DECODE_FAILED("decode_failed"),
        CORRELATION_DROP("correlation_drop"),
        CONTEXT_TABLE_DROP("context_table_drop"),
        STACK_BUDGET_DROP("stack_budget_drop"),
        STACK_CAPTURE_FAILED("stack_capture_failed"),
        CANONICAL_BATCH_DROP("canonical_batch_drop"),
        EVENT_SINK_DROP("event_sink_drop"),
        METHOD_RESOLUTION_DROP("method_resolution_drop"),
        FINAL_DRAIN_FAILED("final_drain_failed"),
        FINAL_DRAIN_TRUNCATED("final_drain_truncated"),
        SHUTDOWN_TIMEOUT("shutdown_timeout", true),
        NATIVE_SHUTDOWN_INCOMPLETE("native_shutdown_incomplete", true),
        STOPPED("stopped"),
    }

    private companion object {
        const val TAG = "JankHunter"
        const val MIN_ATTACH_API = 28
        const val CONTROL_THREAD_NAME = "JankHunterArtTiControl"
        const val NATIVE_LIBRARY_FILE = "libjankhunter_artti.so"
        const val MAX_STOP_WAIT_MS = 1_000L
        const val MAX_CONSECUTIVE_DRAIN_ERRORS = 3
        const val MAX_FAILURE_CLASS_LENGTH = 48
        const val NANOS_PER_MS = 1_000_000L
        const val STACK_TRIGGER_MAIN_THREAD_STALL = 1
        const val STACK_TRIGGER_LONG_CONTENTION = 2
        const val EVENT_AGENT_STATUS = 1
        const val EVENT_CAPABILITY = 2
        const val EVENT_QUALITY = 3
        const val EVENT_THREAD_END = 5
        const val EVENT_MONITOR_CONTENTION = 7
        const val EVENT_STACK_DEFINITION = 9
        const val EVENT_CORRELATION_LINK = 11
        const val FNV_OFFSET_BASIS = -3750763034362895579L
        const val FNV_PRIME = 1099511628211L
        const val NULL_MARKER = 0xffL
        const val FIELD_SEPARATOR = 0xfeL
        const val MAX_METHOD_SYMBOL_LENGTH = 2_048
        const val MAX_FINAL_DRAIN_BATCHES = 64
        const val SDK_STATUS_CONFIG_APPLIED = 0x100
        const val SDK_STATUS_REASON_BASE = 0x1_000
    }
}
