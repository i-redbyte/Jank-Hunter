package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.io.AsyncWriterProducer
import io.jankhunter.runtime.internal.io.MetricAggregator
import java.nio.file.Files
import java.util.concurrent.atomic.AtomicLong
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeMetricsServiceTest {
    @Test
    fun resetReleasesPendingMetricKeys() {
        withService { service, _, _, _ ->
            service.recordCounter("owner.retained.until.reset.count", 1L)

            service.reset()

            val aggregator = RuntimeMetricsService::class.java.getDeclaredField("aggregator").run {
                isAccessible = true
                get(service) as MetricAggregator
            }
            val active = MetricAggregator::class.java.getDeclaredField("active").run {
                isAccessible = true
                get(aggregator)
            }
            val counters = active.javaClass.getDeclaredField("counters").run {
                isAccessible = true
                get(active) as Map<*, *>
            }
            assertTrue(counters.isEmpty())
        }
    }

    @Test
    fun executorStartUsesOneAggregationGenerationAndOneSchedulingPass() {
        var configReads = 0
        withService(onConfigRead = { configReads++ }) { service, _, delayed, _ ->
            service.recordExecutorStarted(
                keys = ExecutorMetricKeys("decode", "gallery"),
                waitMs = 7L,
                queueDepth = 2,
                activeCount = 1,
                poolSize = 4,
                completedTaskCount = 11L,
            )

            val generation = RuntimeMetricsService::class.java.getDeclaredField("metricGeneration").run {
                isAccessible = true
                get(service) as AtomicLong
            }
            assertEquals(1L, generation.get())
            assertEquals(2, configReads)
            assertEquals(1, delayed.size)
        }
    }

    @Test
    fun alreadyQueuedWindowSkipsClockAndConfigurationWork() {
        var clockReads = 0
        withService(onClockRead = { clockReads++ }) { service, _, _, _ ->
            service.recordCounter("first", 1L)
            service.recordCounter("second", 1L)

            assertEquals(2, clockReads)
        }
    }

    @Test
    fun maintenanceSchedulingUsesPrimitiveResultPorts() {
        val fields = RuntimeMetricsService::class.java.declaredFields.associateBy { it.name }

        assertFalse(fields.getValue("executeMaintenance").type == Function1::class.java)
        assertFalse(fields.getValue("executeDelayedMaintenance").type == Function2::class.java)
        assertFalse(fields.getValue("executeBlockingDrain").type == Function2::class.java)
    }

    @Test
    fun firstSampleSchedulesOneDelayedWindowFlush() {
        withService { service, immediate, delayed, _ ->
            service.recordCounter("first", 1L)
            service.recordCounter("second", 1L)

            assertTrue(immediate.isEmpty())
            assertEquals(1, delayed.size)
            assertEquals(WINDOW_MS, delayed.single().delayMs)

            delayed.single().task()
        }
    }

    @Test
    fun immediateFlushIsAsynchronousAndCoalesced() {
        withService { service, immediate, _, writer ->
            service.recordCounter("first", 1L)

            assertTrue(service.requestFlush())
            assertTrue(service.requestFlush())
            assertEquals(1, immediate.size)

            immediate.single().invoke()
            assertTrue(writer.flushBlocking())
        }
    }

    @Test
    fun criticalLifecycleMetricsBypassTheBulkAggregator() {
        var contextUpdates = 0
        withService(ensureContextRecorded = { contextUpdates++ }) { service, immediate, delayed, writer ->
            service.recordCounter("app.lifecycle.ui_visible.count", 1L)
            service.recordGauge("screen.checkout.lifecycle.time_to_resume_ms", 120L)

            assertTrue(immediate.isEmpty())
            assertTrue(delayed.isEmpty())
            assertEquals(2, contextUpdates)
            assertTrue(writer.flushBlocking())
        }
    }

    @Test
    fun exactBlockingFlushDoesNotDependOnMaintenanceQueue() {
        withService(maintenanceWaitAccepted = false) { service, immediate, _, writer ->
            service.recordCounter("exact.metric", 1L)

            assertEquals(0L, acceptedEvents(writer))
            assertTrue(service.flushBlocking(0L))
            assertEquals(1L, acceptedEvents(writer))
            assertTrue(immediate.isEmpty())
            assertTrue(writer.flushBlocking())
        }
    }

    @Test
    fun bestEffortBlockingFlushDoesNotDependOnMaintenanceQueue() {
        withService(maintenanceWaitAccepted = false, exactAdmission = false) { service, immediate, _, writer ->
            service.recordCounter("best.effort.metric", 1L)

            assertEquals(0L, acceptedEvents(writer))
            assertTrue(service.flushBlocking(0L))
            assertEquals(1L, acceptedEvents(writer))
            assertTrue(immediate.isEmpty())
            assertTrue(writer.flushBlocking())
        }
    }

    private fun withService(
        ensureContextRecorded: () -> Unit = {},
        maintenanceWaitAccepted: Boolean = true,
        onClockRead: () -> Unit = {},
        onConfigRead: () -> Unit = {},
        exactAdmission: Boolean = true,
        block: (
            service: RuntimeMetricsService,
            immediate: MutableList<() -> Unit>,
            delayed: MutableList<DelayedTask>,
            writer: AsyncLogWriter,
        ) -> Unit,
    ) {
        val directory = Files.createTempDirectory("jankhunter-runtime-metrics").toFile()
        val config = JankHunterConfig.builder()
            .metricAggregationEnabled(true)
            .metricAggregationWindowMs(WINDOW_MS)
            .maxMetricAggregationKeys(16)
            .build()
        val writer = AsyncLogWriterFactory().open(directory, config, "main")
        val immediate = mutableListOf<() -> Unit>()
        val delayed = mutableListOf<DelayedTask>()
        var nowMs = 10_000L
        val service = RuntimeMetricsService(
            defaultMaxKeys = 16,
            nowMs = {
                onClockRead()
                nowMs
            },
            writer = { writer },
            config = {
                onConfigRead()
                config
            },
            ensureContextRecorded = ensureContextRecorded,
            executeMaintenance = { task ->
                if (maintenanceWaitAccepted) immediate += task
                maintenanceWaitAccepted
            },
            executeDelayedMaintenance = { delayMs, task ->
                delayed += DelayedTask(delayMs, task)
                true
            },
            // Routing tests control this lane. Real worker/deadline behavior is tested separately.
            executeBlockingDrain = { task -> task(); true },
        )
        service.configure(16, exactAdmission = exactAdmission)
        try {
            block(service, immediate, delayed, writer)
        } finally {
            nowMs += WINDOW_MS
            service.flushBlocking(1_000L)
            writer.close()
            directory.deleteRecursively()
        }
    }

    private fun acceptedEvents(writer: AsyncLogWriter): Long {
        val field = AsyncLogWriter::class.java.getDeclaredField("producer").apply { isAccessible = true }
        return (field.get(writer) as AsyncWriterProducer).acceptedSequence
    }

    private data class DelayedTask(
        val delayMs: Long,
        val task: () -> Unit,
    )

    private companion object {
        const val WINDOW_MS = 5_000L
    }
}
