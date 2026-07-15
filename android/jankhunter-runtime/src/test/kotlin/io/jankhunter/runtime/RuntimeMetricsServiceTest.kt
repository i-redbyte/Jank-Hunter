package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeMetricsServiceTest {
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
            service.recordCounter("app.lifecycle.foreground.count", 1L)
            service.recordGauge("screen.checkout.lifecycle.time_to_resume_ms", 120L)

            assertTrue(immediate.isEmpty())
            assertTrue(delayed.isEmpty())
            assertEquals(2, contextUpdates)
            assertTrue(writer.flushBlocking())
        }
    }

    private fun withService(
        ensureContextRecorded: () -> Unit = {},
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
        val writer = AsyncLogWriter.open(directory, config, "main")
        val immediate = mutableListOf<() -> Unit>()
        val delayed = mutableListOf<DelayedTask>()
        var nowMs = 10_000L
        val service = RuntimeMetricsService(
            defaultMaxKeys = 16,
            nowMs = { nowMs },
            writer = { writer },
            config = { config },
            ensureContextRecorded = ensureContextRecorded,
            executeMaintenance = { task ->
                immediate += task
                true
            },
            executeDelayedMaintenance = { delayMs, task ->
                delayed += DelayedTask(delayMs, task)
                true
            },
            executeMaintenanceAndWait = { _, task ->
                task()
                true
            },
        )
        service.configure(16)
        try {
            block(service, immediate, delayed, writer)
        } finally {
            nowMs += WINDOW_MS
            service.flushBlocking(1_000L)
            writer.close()
            directory.deleteRecursively()
        }
    }

    private data class DelayedTask(
        val delayMs: Long,
        val task: () -> Unit,
    )

    private companion object {
        const val WINDOW_MS = 5_000L
    }
}
