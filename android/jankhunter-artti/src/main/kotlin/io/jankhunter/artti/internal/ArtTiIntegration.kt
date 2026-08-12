package io.jankhunter.artti.internal

import android.annotation.SuppressLint
import android.content.Context
import android.content.pm.ApplicationInfo
import android.os.Build
import android.os.Debug
import android.os.SystemClock
import android.util.Log
import io.jankhunter.runtime.JankHunter
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

    @Volatile
    private var worker: Thread? = null

    @Volatile
    private var config: ArtTiRuntimeConfig? = null

    @Volatile
    private var triggerBudget: ArtTiTriggerBudget? = null

    override fun start(context: Context) {
        while (true) {
            val observed = lifecycle.get()
            if (observed != Lifecycle.UNINITIALIZED && observed != Lifecycle.STOPPED) return
            if (lifecycle.compareAndSet(observed, Lifecycle.ATTACHING)) break
        }
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
            verifyHandshake(runtimeConfig)
            phase = ControlPhase.ACTIVATE
            if (!lifecycle.compareAndSet(Lifecycle.ATTACHING, Lifecycle.ACTIVE)) return
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
            }
            worker = null
            if (lifecycle.get() == Lifecycle.STOPPING) lifecycle.set(Lifecycle.STOPPED)
        }
    }

    private fun verifyHandshake(runtimeConfig: ArtTiRuntimeConfig) {
        val buffer = ArtTiNativeProtocol.allocateHandshakeBuffer()
        val status = ArtTiNativeStatus.fromWire(ArtTiNativeBridge.nativeHandshake(buffer))
        require(status == ArtTiNativeStatus.OK) { "native_handshake_failed" }
        val handshake = ArtTiNativeHandshake.decode(buffer).getOrThrow()
        require(handshake.isCompatible()) { "native_handshake_incompatible" }
        require(handshake.configHash == runtimeConfig.native.configHash) { "native_config_hash_mismatch" }
    }

    private fun drainLoop(runtimeConfig: ArtTiRuntimeConfig) {
        val output = ArtTiNativeProtocol.allocateDrainBuffer(runtimeConfig.native.drainBatchSize)
        val decoder = ArtTiNativeProtocolDecoder()
        val contexts = ArtTiThreadContextTable(runtimeConfig.native.maxTrackedThreads)
        val visitor = ArtTiNativeRecordVisitor { record -> onNativeRecord(record, runtimeConfig, contexts) }
        var consecutiveErrors = 0
        while (isActive()) {
            val bytesWritten = ArtTiNativeBridge.nativeDrain(output, runtimeConfig.native.drainBatchSize)
            if (bytesWritten < 0) {
                consecutiveErrors++
                report(Reason.DRAIN_FAILED)
                if (consecutiveErrors >= MAX_CONSECUTIVE_DRAIN_ERRORS) error("native_drain_failed")
            } else {
                val result = decoder.decode(output, bytesWritten, visitor)
                if (result.status == ArtTiNativeStatus.OK) {
                    consecutiveErrors = 0
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

    private fun onNativeRecord(
        record: ArtTiNativeRecordView,
        runtimeConfig: ArtTiRuntimeConfig,
        contexts: ArtTiThreadContextTable,
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
            EVENT_THREAD_END -> contexts.remove(record.threadToken)
            EVENT_MONITOR_CONTENTION -> maybeCaptureLongContention(record, runtimeConfig, contexts)
            EVENT_CORRELATION_LINK -> if (!contexts.put(record.threadToken, record.contextToken)) {
                report(Reason.CONTEXT_TABLE_DROP)
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
        const val EVENT_THREAD_END = 5
        const val EVENT_MONITOR_CONTENTION = 7
        const val EVENT_CORRELATION_LINK = 11
        const val FNV_OFFSET_BASIS = -3750763034362895579L
        const val FNV_PRIME = 1099511628211L
        const val NULL_MARKER = 0xffL
        const val FIELD_SEPARATOR = 0xfeL
    }
}
