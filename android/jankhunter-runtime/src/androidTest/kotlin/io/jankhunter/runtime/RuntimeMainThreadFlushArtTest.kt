package io.jankhunter.runtime

import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.io.File
import java.util.UUID
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Test

class RuntimeMainThreadFlushArtTest {
    @Test
    fun uiFlushDoesNotDispatchOrWaitForABlockingMetricDrain() {
        val directory = File(InstrumentationRegistry.getInstrumentation().targetContext.cacheDir,
            "ui-flush-${UUID.randomUUID()}")
        val config = JankHunterConfig.builder().metricAggregationEnabled(true).build()
        val writer = AsyncLogWriterFactory().open(directory, config, "app")
        val blockingDrains = AtomicInteger()
        val graph = RuntimeComponentGraph({ 1L }, { 1L }, blockingMetricDrains = {
            blockingDrains.incrementAndGet()
            false
        })
        graph.state.config = config
        graph.state.writer = writer
        graph.metrics.configure(16, true)
        graph.metrics.recordCounter("pending.ui.metric", 1L)
        try {
            InstrumentationRegistry.getInstrumentation().runOnMainSync { graph.session.flush() }
            assertEquals("UI flush entered a synchronous drain", 0, blockingDrains.get())
        } finally {
            graph.metrics.reset()
            writer.close(1_000L)
            directory.deleteRecursively()
        }
    }
}
