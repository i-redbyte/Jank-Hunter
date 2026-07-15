package io.jankhunter.runtime.internal.io

import android.os.Process
import android.os.SystemClock

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
        private val processName: String?,
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
    ) : PendingLogEvent(JhlogV9.TYPE_SESSION, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.session(
                appVersion,
                build,
                device,
                sdkInt,
                processName,
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
    ) : PendingLogEvent(JhlogV9.TYPE_DEVICE_CONTEXT, producerContext) {
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
        private val durationMs: Long,
        private val dnsMs: Long,
        private val connectMs: Long,
        private val ttfbMs: Long,
        private val statusClass: Int,
        private val rxBytes: Long,
        private val txBytes: Long,
        private val flags: Long,
    ) : PendingLogEvent(JhlogV9.TYPE_HTTP, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.http(owner, route, durationMs, dnsMs, connectMs, ttfbMs, statusClass, rxBytes, txBytes, flags)
        }
    }

    class UiWindow(
        producerContext: LogEventContext?,
        private val screen: String?,
        private val windowMs: Long,
        private val frameCount: Long,
        private val jankCount: Long,
        private val p50Ms: Long,
        private val p95Ms: Long,
        private val p99Ms: Long,
        private val foreground: Boolean,
        private val flags: Long,
    ) : PendingLogEvent(JhlogV9.TYPE_UI_WINDOW, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.uiWindow(screen, windowMs, frameCount, jankCount, p50Ms, p95Ms, p99Ms, foreground, flags)
        }
    }

    class Stall(
        producerContext: LogEventContext?,
        private val screen: String?,
        private val owner: String?,
        private val flow: String?,
        private val step: String?,
        private val stackHint: String?,
        private val durationMs: Long,
        private val foreground: Boolean,
    ) : PendingLogEvent(JhlogV9.TYPE_STALL, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.stall(screen, owner, flow, step, stackHint, durationMs, foreground)
        }
    }

    class Memory(
        producerContext: LogEventContext?,
        private val pssKb: Long,
        private val javaHeapKb: Long,
        private val nativeHeapKb: Long,
        private val foreground: Boolean,
    ) : PendingLogEvent(JhlogV9.TYPE_MEMORY, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.memory(pssKb, javaHeapKb, nativeHeapKb, foreground)
        }
    }

    class Retained(
        producerContext: LogEventContext?,
        private val screen: String?,
        private val owner: String?,
        private val flow: String?,
        private val step: String?,
        private val className: String?,
        private val holder: String?,
        private val ageMs: Long,
        private val count: Long,
        private val foreground: Boolean,
        private val evidence: Long,
    ) : PendingLogEvent(JhlogV9.TYPE_RETAINED, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.retained(screen, owner, flow, step, className, holder, ageMs, count, foreground, evidence)
        }
    }

    class Counter(
        producerContext: LogEventContext?,
        private val name: String?,
        private val value: Long,
    ) : PendingLogEvent(JhlogV9.TYPE_COUNTER, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) = writer.counter(name, value)
    }

    class StableCounters(
        producerContext: LogEventContext?,
        private val batch: StableCounterBatch,
    ) : PendingLogEvent(JhlogV9.TYPE_COUNTER, producerContext) {
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
    ) : PendingLogEvent(JhlogV9.TYPE_GAUGE, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) = writer.gauge(name, value, count, sum, max, mode)
    }

    class Flow(
        producerContext: LogEventContext?,
        private val screen: String?,
        private val owner: String?,
        private val flow: String?,
        private val step: String?,
    ) : PendingLogEvent(JhlogV9.TYPE_FLOW_TRANSITION, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) = writer.flowContext(screen, owner, flow, step)
    }

    class LogSpam(
        producerContext: LogEventContext?,
        private val screen: String?,
        private val owner: String?,
        private val flow: String?,
        private val step: String?,
        private val source: String?,
        private val level: Int,
        private val count: Long,
    ) : PendingLogEvent(JhlogV9.TYPE_LOG_SPAM, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.logSpam(screen, owner, flow, step, source, level, count)
        }
    }

    class Problem(
        producerContext: LogEventContext?,
        private val screen: String?,
        private val owner: String?,
        private val flow: String?,
        private val step: String?,
        private val kind: String?,
        private val windowMs: Long,
        private val count: Long,
        private val maxMs: Long,
        private val foreground: Boolean,
    ) : PendingLogEvent(JhlogV9.TYPE_PROBLEM, producerContext) {
        override fun writePayload(writer: BinaryLogWriter) {
            writer.problemWindow(screen, owner, flow, step, kind, windowMs, count, maxMs, foreground)
        }
    }

    class RuntimeCalls(
        producerContext: LogEventContext?,
        private val batch: RuntimeCallBatch,
    ) : PendingLogEvent(JhlogV9.TYPE_RUNTIME_CALL, producerContext) {
        private var written = 0

        override val logicalEventCount: Long
            get() = batch.size.toLong()

        override val remainingEventCount: Long
            get() = (batch.size - written).coerceAtLeast(0).toLong()

        override fun writePayload(writer: BinaryLogWriter) {
            while (written < batch.size) {
                val index = written
                writer.runtimeCall(
                    batch.screen(index),
                    batch.callerId(index),
                    batch.callerName(index),
                    batch.flow(index),
                    batch.step(index),
                    batch.calleeId(index),
                    batch.calleeName(index),
                    batch.count(index),
                    batch.totalMs(index),
                    batch.maxMs(index),
                )
                written++
            }
        }
    }
}
