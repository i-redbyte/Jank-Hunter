package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import java.nio.file.Files
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeCoordinatorTest {
    @Test
    fun hooksRequireStartedWriterAndRuntimeFlag() {
        val state = RuntimeState()
        val coordinator = RuntimeCoordinator(state) { 1L }
        val directory = Files.createTempDirectory("jankhunter-runtime-coordinator").toFile()

        try {
            assertFalse(coordinator.isActiveForHooks())

            state.started.set(true)
            assertFalse(coordinator.isActiveForHooks())

            state.writer = AsyncLogWriter.open(
                directory,
                JankHunterConfig.builder()
                    .autoStartCollectors(false)
                    .build(),
                "main",
            )
            assertTrue(coordinator.isActiveForHooks())

            state.runtimeEnabled.set(false)
            assertFalse(coordinator.isActiveForHooks())

            state.runtimeEnabled.set(true)
            state.writer?.close()
            assertFalse(coordinator.isActiveForHooks())
        } finally {
            state.writer?.close()
            directory.deleteRecursively()
        }
    }
}
