package io.jankhunter.runtime.internal.io

import android.os.Process
import android.os.SystemClock
import io.jankhunter.runtime.JankHunterHttpEvent
import io.jankhunter.runtime.JankHunterOperationAttributes
import io.jankhunter.runtime.JankHunterWebSocketEvent

/**
 * Immutable data accepted by the asynchronous writer.
 *
 * Keeping queue entries typed makes admission/loss accounting deterministic and prevents queued
 * closures from retaining arbitrary application objects. Every entry owns the producer metadata
 * captured before admission.
 */
internal sealed class PendingLogEvent(
    val recordType: Int,
    private val producerContext: LogEventContext?,
) {
    private val producerElapsedUs = SystemClock.elapsedRealtimeNanos().coerceAtLeast(0L) / 1_000L
    private val producerThreadId = Process.myTid().toLong().coerceAtLeast(0L)

    /** Global admission order used to merge the independently bounded writer lanes. */
    var sequence: Long = 0L
        internal set

    open val logicalEventCount: Long = 1L

    open val remainingEventCount: Long
        get() = logicalEventCount

    fun writeTo(writer: BinaryLogWriter) {
        val event = this
        writer.withProducer(producerElapsedUs, producerThreadId, producerContext) {
            event.writePayload(this)
        }
    }

    protected abstract fun writePayload(writer: BinaryLogWriter)

    class Session(
        producerContext: LogEventContext?,
        private val appVersion: String?,
        private val build: String?,
        private val device: String?,
        private val sdkInt: Int,
        private val androidRelease: String?,
        private val securityPatch: String?,
        private val primaryAbi: String?,
        private val supportedAbis: String?,
        private val manufacturer: String?,
        private val brand: String?,
        private val hardware: String?,
        private val board: String?,
        private val product: String?,
        private val deviceRooted: Boolean,
        private val collectorFlags: Long,
    ) : PendingLogEvent(Jhlog.TYPE_SESSION, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.session(
                appVersion,
                build,
                device,
                sdkInt,
                androidRelease,
                securityPatch,
                primaryAbi,
                supportedAbis,
                manufacturer,
                brand,
                hardware,
                board,
                product,
                deviceRooted,
                collectorFlags,
                appForeground = false,
            )
        }
    }

    class DeviceContext(
        producerContext: LogEventContext?,
        private val networkKind: Int,
        private val batteryPct: Int,
        private val availMemoryKb: Long,
        private val batteryState: Int,
        private val batteryTempDeciC: Int,
        private val lowMemory: Boolean,
        private val networkMetered: Boolean,
        private val networkValidated: Boolean,
        private val rxBytes: Long,
        private val txBytes: Long,
        private val totalMemoryKb: Long,
        private val freeStorageKb: Long,
        private val totalStorageKb: Long,
        private val networkVpn: Boolean,
        private val foreground: Boolean,
    ) : PendingLogEvent(Jhlog.TYPE_DEVICE_CONTEXT, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.context(
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
                foreground,
            )
        }
    }

    class Http(
        producerContext: LogEventContext?,
        private val owner: String?,
        private val route: String?,
        private val event: JankHunterHttpEvent,
        private val flags: Long,
    ) : PendingLogEvent(Jhlog.TYPE_HTTP, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.http(owner, route, event, flags)
        }
    }

    class UiWindow(
        producerContext: LogEventContext?,
        private val screen: String?,
        private val windowMs: Long,
        private val frameCount: Long,
        private val jankCount: Long,
        private val source: Long,
        private val frameDeadlineUs: Long,
        private val frameDurationBuckets: LongArray,
        private val foreground: Boolean,
        private val flags: Long,
    ) : PendingLogEvent(Jhlog.TYPE_UI_WINDOW, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.uiWindow(
                screen,
                windowMs,
                frameCount,
                jankCount,
                source,
                frameDeadlineUs,
                frameDurationBuckets,
                foreground,
                flags,
            )
        }
    }

    class ProcessExit(
        producerContext: LogEventContext?,
        private val reason: Long,
        private val timestampUnixMs: Long,
        private val importance: Long,
        private val pssKb: Long,
        private val rssKb: Long,
        private val processName: String?,
    ) : PendingLogEvent(Jhlog.TYPE_PROCESS_EXIT, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.processExit(reason, timestampUnixMs, importance, pssKb, rssKb, processName)
        }
    }

    class IO(
        producerContext: LogEventContext?,
        private val operation: Long,
        private val durationUs: Long,
        private val bytes: Long,
        private val mainThread: Boolean,
        private val sourceId: Long,
        private val sourceName: String?,
        private val outcome: Long,
        private val bytesKnown: Boolean,
    ) : PendingLogEvent(Jhlog.TYPE_IO, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.io(operation, durationUs, bytes, mainThread, sourceId, sourceName, outcome, bytesKnown)
        }
    }

    class Worker(
        producerContext: LogEventContext?,
        private val workerId: Long,
        private val workerName: String?,
        private val instanceId: Long,
        private val stage: Long,
        private val outcome: Long,
        private val durationMs: Long,
        private val runAttempt: Long,
        private val generation: Long,
        private val stopReason: Long,
        private val flags: Long,
    ) : PendingLogEvent(Jhlog.TYPE_WORKER, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.worker(
                workerId,
                workerName,
                instanceId,
                stage,
                outcome,
                durationMs,
                runAttempt,
                generation,
                stopReason,
                flags,
            )
        }
    }

    class WebSocket(
        producerContext: LogEventContext?,
        private val owner: String?,
        private val event: JankHunterWebSocketEvent,
    ) : PendingLogEvent(Jhlog.TYPE_WEBSOCKET, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.webSocket(owner, event)
        }
    }

    class Database(
        producerContext: LogEventContext?,
        private val sourceId: Long,
        private val sourceName: String?,
        private val query: String?,
        private val framework: Long,
        private val operation: Long,
        private val outcome: Long,
        private val durationUs: Long,
        private val mainThread: Boolean,
        private val failureKind: Long,
        private val boundary: Long,
        private val statementFingerprint: Long,
        private val resultKnown: Boolean,
        private val resultKind: Long,
        private val resultCountBucket: Long,
        private val transactionId: Long,
        private val statementToken: Long,
        private val phaseMask: Long,
        private val poolWaitUs: Long,
        private val lockWaitUs: Long,
        private val executeUs: Long,
        private val materializeUs: Long,
    ) : PendingLogEvent(Jhlog.TYPE_DATABASE, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.database(
                sourceId, sourceName, query, framework, operation, outcome, durationUs, mainThread,
                failureKind, boundary, statementFingerprint, resultKnown, resultKind, resultCountBucket,
                transactionId, statementToken, phaseMask, poolWaitUs, lockWaitUs, executeUs, materializeUs,
            )
        }
    }

    class DatabaseTransaction(
        producerContext: LogEventContext?,
        private val sourceId: Long,
        private val sourceName: String?,
        private val transactionId: Long,
        private val parentId: Long,
        private val stage: Long,
        private val mode: Long,
        private val outcome: Long,
        private val failureKind: Long,
        private val durationUs: Long,
        private val statementCount: Long,
        private val readCount: Long,
        private val writeCount: Long,
        private val mainThread: Boolean,
    ) : PendingLogEvent(Jhlog.TYPE_DATABASE_TRANSACTION, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.databaseTransaction(
                sourceId, sourceName, transactionId, parentId, stage, mode, outcome, failureKind,
                durationUs, statementCount, readCount, writeCount, mainThread,
            )
        }
    }

    class ProcessState(
        producerContext: LogEventContext?,
        private val uiVisibility: Long,
        private val processImportance: Long,
        private val androidImportance: Long,
        private val reason: Long,
    ) : PendingLogEvent(Jhlog.TYPE_PROCESS_STATE, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.processState(uiVisibility, processImportance, androidImportance, reason)
        }
    }

    class AndroidComponent(
        producerContext: LogEventContext?,
        private val componentId: Long,
        private val componentName: String?,
        private val action: String?,
        private val instanceId: Long,
        private val flowId: Long,
        private val kind: Long,
        private val stage: Long,
        private val outcome: Long,
        private val durationUs: Long,
        private val componentFlags: Long,
    ) : PendingLogEvent(Jhlog.TYPE_ANDROID_COMPONENT, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.androidComponent(
                componentId,
                componentName,
                action,
                instanceId,
                flowId,
                kind,
                stage,
                outcome,
                durationUs,
                componentFlags,
            )
        }
    }

    class BinderTransaction(
        producerContext: LogEventContext?,
        private val descriptor: String?,
        private val method: String?,
        private val callId: Long,
        private val direction: Long,
        private val transactionCode: Long,
        private val outcome: Long,
        private val failureKind: Long,
        private val durationUs: Long,
        private val binderFlags: Long,
        private val mainThread: Boolean,
    ) : PendingLogEvent(Jhlog.TYPE_BINDER_TRANSACTION, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.binderTransaction(
                descriptor,
                method,
                callId,
                direction,
                transactionCode,
                outcome,
                failureKind,
                durationUs,
                binderFlags,
                mainThread,
            )
        }
    }

    class Operation(
        producerContext: LogEventContext?,
        private val name: String,
        private val operationId: Long,
        private val parentId: Long,
        private val phase: Long,
        private val kind: Long,
        private val outcome: Long,
        private val durationUs: Long,
        private val budgetUs: Long,
        private val attributes: JankHunterOperationAttributes,
    ) : PendingLogEvent(Jhlog.TYPE_OPERATION, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.operation(
                name,
                operationId,
                parentId,
                phase,
                kind,
                outcome,
                durationUs,
                budgetUs,
                attributes,
            )
        }
    }

    class Stall(
        producerContext: LogEventContext?,
        private val screen: String?,
        private val owner: String?,
        private val stackHint: String?,
        private val durationMs: Long,
        private val foreground: Boolean,
    ) : PendingLogEvent(Jhlog.TYPE_STALL, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.stall(screen, owner, stackHint, durationMs, foreground)
        }
    }

    class Memory(
        producerContext: LogEventContext?,
        private val pssKb: Long,
        private val javaHeapKb: Long,
        private val nativeHeapKb: Long,
        private val foreground: Boolean,
    ) : PendingLogEvent(Jhlog.TYPE_MEMORY, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.memory(pssKb, javaHeapKb, nativeHeapKb, foreground)
        }
    }

    class Retained(
        producerContext: LogEventContext?,
        private val screen: String?,
        private val owner: String?,
        private val className: String?,
        private val holder: String?,
        private val ageMs: Long,
        private val count: Long,
        private val foreground: Boolean,
        private val evidence: Long,
    ) : PendingLogEvent(Jhlog.TYPE_RETAINED, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.retained(screen, owner, className, holder, ageMs, count, foreground, evidence)
        }
    }

    class Counter(
        producerContext: LogEventContext?,
        private val name: String?,
        private val value: Long,
    ) : PendingLogEvent(Jhlog.TYPE_COUNTER, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) = writer.counter(name, value)
    }

    class StableCounters(
        producerContext: LogEventContext?,
        private val batch: StableCounterBatch,
    ) : PendingLogEvent(Jhlog.TYPE_COUNTER, producerContext) {
        private var written = 0

        override val logicalEventCount: Long
            get() = batch.size.toLong()

        override val remainingEventCount: Long
            get() = (batch.size - written).coerceAtLeast(0).toLong()

        override fun writePayload(writer: BinaryLogWriter) {
            while (written < batch.size) {
                val index = written
                writer.stableCounter(batch.id(index), batch.name(index), batch.value(index))
                written++
            }
        }
    }

    class Gauge(
        producerContext: LogEventContext?,
        private val name: String?,
        private val value: Long,
        private val count: Long,
        private val sum: Long,
        private val max: Long,
        private val mode: MetricAggregationMode,
    ) : PendingLogEvent(Jhlog.TYPE_GAUGE, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) = writer.gauge(name, value, count, sum, max, mode)
    }

    class LogSpam(
        producerContext: LogEventContext?,
        private val screen: String?,
        private val owner: String?,
        private val operationId: Long,
        private val source: String?,
        private val level: Int,
        private val count: Long,
    ) : PendingLogEvent(Jhlog.TYPE_LOG_SPAM, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.logSpam(screen, owner, operationId, source, level, count)
        }
    }

    class Problem(
        producerContext: LogEventContext?,
        private val screen: String?,
        private val owner: String?,
        private val kind: String?,
        private val windowMs: Long,
        private val count: Long,
        private val maxMs: Long,
        private val foreground: Boolean,
    ) : PendingLogEvent(Jhlog.TYPE_PROBLEM, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.problemWindow(screen, owner, kind, windowMs, count, maxMs, foreground)
        }
    }

    class RuntimeCalls(
        producerContext: LogEventContext?,
        private val batch: RuntimeCallBatch,
    ) : PendingLogEvent(Jhlog.TYPE_RUNTIME_CALL, producerContext) {
        private var written = 0

        override val logicalEventCount: Long
            get() = batch.size.toLong()

        override val remainingEventCount: Long
            get() = (batch.size - written).coerceAtLeast(0).toLong()

        override fun writePayload(writer: BinaryLogWriter) {
            if (written >= batch.size) return
            writer.runtimeCalls(batch)
            written = batch.size
        }
    }
}
