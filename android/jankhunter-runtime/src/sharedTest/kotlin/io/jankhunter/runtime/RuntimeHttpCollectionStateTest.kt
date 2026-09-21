package io.jankhunter.runtime

import java.nio.file.Files
import java.util.concurrent.atomic.AtomicLong
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.io.LogQualityCounters
import org.junit.Assert.assertEquals
import org.junit.Test

class RuntimeHttpCollectionStateTest {
    @Test
    fun collectionBoundsExcludeStartupAndShutdownAndAreRecordedOnce() {
        val directory = Files.createTempDirectory("jh-http-window").toFile()
        val clock = AtomicLong(100L)
        val graph = RuntimeComponentGraph(nowMs = clock::get, nowUs = { clock.get() * 1_000L })
        val config = JankHunterConfig.builder().autoStartCollectors(false).build()
        val writer = AsyncLogWriterFactory().open(directory, config, "main")
        val quality = writer.javaClass.getDeclaredField("quality").apply { isAccessible = true }.get(writer) as LogQualityCounters
        try {
            graph.state.writer = writer
            graph.coordinator.markStarted(config)
            assertEquals("start is rounded up after hook activation", 101L, quality.value(0x204b))
            clock.set(200L)
            graph.coordinator.disableHooks()
            clock.set(300L)
            graph.coordinator.beginStop()
            assertEquals("end precedes hook deactivation, not writer drain", 200L, quality.value(0x204c))
            graph.coordinator.markStopped()
            writer.close()
            assertEquals("repeated stop and writer callback must not add timestamps", 200L, quality.value(0x204c))
        } finally {
            graph.coordinator.markStopped()
            writer.close()
            directory.deleteRecursively()
        }
    }

    @Test
    fun sessionDeclaresEffectiveHttpCollectionState() {
        val session = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1_000L }).session
        val flags = RuntimeSessionController::class.java
            .getDeclaredMethod("collectorFlags", JankHunterConfig::class.java)
            .apply { isAccessible = true }
        for (available in listOf(false, true)) {
            for (enabled in listOf(false, true)) {
                val config = JankHunterConfig.builder()
                    .availableRuntimeFeatures(if (available) setOf(JankHunterRuntimeFeature.HTTP) else emptySet())
                    .runtimeFeatureEnabled(JankHunterRuntimeFeature.HTTP, enabled)
                    .build()
                assertEquals("available=$available enabled=$enabled", available && enabled,
                    (flags.invoke(session, config) as Long) and (1L shl 11) != 0L)
            }
        }
    }
}
