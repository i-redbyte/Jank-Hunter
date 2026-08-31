package io.jankhunter.runtime

import android.content.BroadcastReceiver
import android.content.Intent
import io.jankhunter.runtime.internal.io.Jhlog
import io.jankhunter.runtime.internal.io.QualityCounterId
import java.util.concurrent.atomic.AtomicLong

/**
 * Allocation-free correlation for component callbacks. A packed primitive token carries both the
 * monotonic start timestamp and a sequence, so no component instance is retained between hooks.
 */
internal class RuntimeAndroidComponentTelemetry(
    private val access: RuntimeTelemetryAccess,
    nowUs: RuntimeLongSource,
) {
    private val tokens = RuntimeTraceTokenSource(nowUs)
    private val pendingReceivers = PendingReceiverRegistry(
        onEviction = {
            access.writer?.recordQuality(QualityCounterId.RECEIVER_ASYNC_REGISTRY_EVICTION)
        },
        onResolutionMissAfterEviction = {
            access.writer?.recordQuality(QualityCounterId.RECEIVER_ASYNC_RESOLUTION_MISS_AFTER_EVICTION)
        },
    )

    fun enterServiceCallback(): Long {
        return if (access.isActive() && access.writer != null) tokens.start() else 0L
    }

    fun exitServiceCallback(
        token: Long,
        service: Any?,
        intent: Any?,
        componentId: Long,
        componentName: String,
        stage: Int,
        resultCode: Int,
        resultObject: Any?,
        failed: Boolean,
    ) {
        if (token == 0L || service == null) return
        val writer = access.writer ?: return
        val nowUs = tokens.nowUs()
        writer.androidComponent(
            componentId = componentId,
            componentName = componentName,
            action = (intent as? Intent)?.action,
            instanceId = componentInstanceId(service),
            flowId = token,
            kind = Jhlog.COMPONENT_KIND_SERVICE,
            stage = stage.toLong(),
            outcome = serviceComponentOutcome(stage, failed),
            durationUs = RuntimeTraceTokenSource.durationUs(token, nowUs),
            componentFlags = serviceComponentFlags(stage, resultCode, resultObject),
        )
        access.recordProcessState(writer, Jhlog.PROCESS_STATE_COMPONENT_LIFECYCLE, forceRefresh = false)
    }

    fun recordServiceForegroundTransition(
        service: Any?,
        componentId: Long,
        componentName: String,
        stage: Int,
    ) {
        if (service == null) return
        val writer = access.writer ?: return
        val flowId = tokens.start()
        writer.androidComponent(
            componentId = componentId,
            componentName = componentName,
            action = null,
            instanceId = componentInstanceId(service),
            flowId = flowId,
            kind = Jhlog.COMPONENT_KIND_SERVICE,
            stage = stage.toLong(),
            outcome = Jhlog.COMPONENT_OUTCOME_SUCCESS,
            durationUs = 0L,
            componentFlags = if (stage.toLong() == Jhlog.COMPONENT_SERVICE_FOREGROUND_ENTERED) {
                Jhlog.COMPONENT_FLAG_FOREGROUND
            } else {
                0L
            },
        )
        access.recordProcessState(writer, Jhlog.PROCESS_STATE_COMPONENT_LIFECYCLE, forceRefresh = true)
    }

    fun enterReceiverCallback(
        receiver: Any?,
        intent: Any?,
        componentId: Long,
        componentName: String,
    ): Long {
        if (receiver == null || !access.isActive()) return 0L
        val writer = access.writer ?: return 0L
        val token = tokens.start()
        val flags = receiverComponentFlags(receiver)
        writer.androidComponent(
            componentId = componentId,
            componentName = componentName,
            action = (intent as? Intent)?.action,
            instanceId = componentInstanceId(receiver),
            flowId = token,
            kind = Jhlog.COMPONENT_KIND_RECEIVER,
            stage = Jhlog.COMPONENT_RECEIVER_STARTED,
            outcome = Jhlog.COMPONENT_OUTCOME_UNKNOWN,
            durationUs = 0L,
            componentFlags = flags,
        )
        access.recordProcessState(writer, Jhlog.PROCESS_STATE_COMPONENT_LIFECYCLE, forceRefresh = false)
        return token
    }

    fun registerReceiverAsync(
        pendingResult: Any?,
        receiver: Any?,
        intent: Any?,
        token: Long,
        componentId: Long,
        componentName: String,
    ): Boolean {
        if (pendingResult == null || receiver == null || token == 0L) return false
        val writer = access.writer ?: return false
        val flags = receiverComponentFlags(receiver) or Jhlog.COMPONENT_FLAG_ASYNC
        val action = (intent as? Intent)?.action
        val instanceId = componentInstanceId(receiver)
        if (!pendingReceivers.register(
                pendingResult,
                PendingReceiverSnapshot(token, componentId, componentName, action, instanceId, flags),
            )
        ) {
            return false
        }
        writer.androidComponent(
            componentId,
            componentName,
            action,
            instanceId,
            token,
            Jhlog.COMPONENT_KIND_RECEIVER,
            Jhlog.COMPONENT_RECEIVER_ASYNC_STARTED,
            Jhlog.COMPONENT_OUTCOME_UNKNOWN,
            RuntimeTraceTokenSource.durationUs(token, tokens.nowUs()),
            flags,
        )
        return true
    }

    fun exitReceiverCallback(
        token: Long,
        receiver: Any?,
        intent: Any?,
        componentId: Long,
        componentName: String,
        asyncStarted: Boolean,
        pendingResult: Any?,
        failed: Boolean,
    ) {
        if (token == 0L || receiver == null) return
        if (asyncStarted && !failed) return
        if (asyncStarted) pendingReceivers.cancel(pendingResult)
        val writer = access.writer ?: return
        writer.androidComponent(
            componentId,
            componentName,
            (intent as? Intent)?.action,
            componentInstanceId(receiver),
            token,
            Jhlog.COMPONENT_KIND_RECEIVER,
            Jhlog.COMPONENT_RECEIVER_FINISHED,
            if (failed) Jhlog.COMPONENT_OUTCOME_FAILURE else Jhlog.COMPONENT_OUTCOME_SUCCESS,
            RuntimeTraceTokenSource.durationUs(token, tokens.nowUs()),
            receiverComponentFlags(receiver, async = asyncStarted),
        )
    }

    fun finishReceiverAsync(pendingResult: Any?) {
        val snapshot = pendingReceivers.complete(pendingResult) ?: return
        val writer = access.writer ?: return
        writer.androidComponent(
            snapshot.componentId,
            snapshot.componentName,
            snapshot.action,
            snapshot.instanceId,
            snapshot.token,
            Jhlog.COMPONENT_KIND_RECEIVER,
            Jhlog.COMPONENT_RECEIVER_FINISHED,
            Jhlog.COMPONENT_OUTCOME_SUCCESS,
            RuntimeTraceTokenSource.durationUs(snapshot.token, tokens.nowUs()),
            snapshot.flags,
        )
        access.recordProcessState(writer, Jhlog.PROCESS_STATE_COMPONENT_LIFECYCLE, forceRefresh = false)
    }
}

internal class RuntimeTraceTokenSource(
    private val nowUsSource: RuntimeLongSource,
) {
    private val sequence = AtomicLong()

    fun start(): Long {
        val timestamp = nowUs() and TIMESTAMP_MASK
        val discriminator = sequence.incrementAndGet() and SEQUENCE_MASK
        return ((discriminator shl TIMESTAMP_BITS) or timestamp).coerceAtLeast(1L)
    }

    fun nowUs(): Long = nowUsSource.getAsLong().coerceAtLeast(0L)

    companion object {
        private const val TIMESTAMP_BITS = 48
        private const val TIMESTAMP_MASK = (1L shl TIMESTAMP_BITS) - 1L
        private const val SEQUENCE_MASK = (1L shl 15) - 1L

        fun durationUs(token: Long, nowUs: Long): Long {
            val startedUs = token and TIMESTAMP_MASK
            return ((nowUs and TIMESTAMP_MASK) - startedUs) and TIMESTAMP_MASK
        }
    }
}

internal fun componentInstanceId(instance: Any): Long {
    return (System.identityHashCode(instance).toLong() and UINT_MASK) + 1L
}

internal fun serviceComponentFlags(stage: Int, resultCode: Int, resultObject: Any?): Long {
    return when (stage.toLong()) {
        Jhlog.COMPONENT_SERVICE_START_COMMAND -> {
            if (resultCode == START_STICKY_COMPATIBILITY || resultCode == START_STICKY ||
                resultCode == START_REDELIVER_INTENT
            ) {
                Jhlog.COMPONENT_FLAG_STICKY
            } else {
                0L
            }
        }
        Jhlog.COMPONENT_SERVICE_BIND -> {
            if (resultObject != null) Jhlog.COMPONENT_FLAG_BOUND else 0L
        }
        Jhlog.COMPONENT_SERVICE_UNBIND,
        Jhlog.COMPONENT_SERVICE_REBIND,
        -> Jhlog.COMPONENT_FLAG_BOUND
        else -> 0L
    }
}

internal fun serviceComponentOutcome(stage: Int, failed: Boolean): Long {
    return when {
        failed -> Jhlog.COMPONENT_OUTCOME_FAILURE
        stage.toLong() == Jhlog.COMPONENT_SERVICE_TIMEOUT -> Jhlog.COMPONENT_OUTCOME_TIMEOUT
        else -> Jhlog.COMPONENT_OUTCOME_SUCCESS
    }
}

internal fun receiverComponentFlags(ordered: Boolean, sticky: Boolean, async: Boolean): Long {
    var flags = 0L
    if (ordered) flags = flags or Jhlog.COMPONENT_FLAG_ORDERED
    if (sticky) flags = flags or Jhlog.COMPONENT_FLAG_STICKY
    if (async) flags = flags or Jhlog.COMPONENT_FLAG_ASYNC
    return flags
}

private fun receiverComponentFlags(receiver: Any, async: Boolean = false): Long {
    val broadcastReceiver = receiver as? BroadcastReceiver ?: return receiverComponentFlags(false, false, async)
    val ordered = RuntimeHookGuard.value(false, RuntimeHookFailureReason.COLLECTOR) {
        broadcastReceiver.isOrderedBroadcast
    }
    val sticky = RuntimeHookGuard.value(false, RuntimeHookFailureReason.COLLECTOR) {
        broadcastReceiver.isInitialStickyBroadcast
    }
    return receiverComponentFlags(ordered, sticky, async)
}

private const val START_STICKY_COMPATIBILITY = 0
private const val START_STICKY = 1
private const val START_REDELIVER_INTENT = 3
private const val UINT_MASK = 0xffff_ffffL
