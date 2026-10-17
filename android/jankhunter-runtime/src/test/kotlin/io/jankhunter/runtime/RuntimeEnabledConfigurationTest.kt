package io.jankhunter.runtime

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assert.assertSame
import org.junit.Assert.assertThrows
import org.junit.Test

class RuntimeEnabledConfigurationTest {
    @Test
    fun lateStorageCompletionWinsOverUnrelatedBindingAndRollback() {
        val state = RuntimeState()
        val original = JankHunterConfig.builder().build()
        state.bindInitialConfiguration(original)
        val initial = state.configurationSnapshot()
        val selected = unusedStorage()
        assertTrue(state.applyBinaryStorage(initial.lifecycleGeneration, 1L, selected))
        val config = original.toBuilder().httpSlowThresholdMs(1234L).build()
        assertSame(selected, state.bindCurrentConfiguration(config).binaryStorage())
        assertSame(selected, state.bindCurrentConfiguration(original, RuntimeStorageUpdate.Restore(0L)).binaryStorage())
        assertSame(selected, state.configurationSnapshot().selectedBinaryStorage)
    }

    @Test
    fun lateStorageCompletionRejectsExplicitReplacementWithoutPublishingOtherFlags() {
        val state = RuntimeState()
        val original = JankHunterConfig.builder().build()
        state.bindInitialConfiguration(original)
        val initial = state.configurationSnapshot()
        val selected = unusedStorage()
        assertTrue(state.applyBinaryStorage(initial.lifecycleGeneration, 1L, selected))
        val before = state.configurationSnapshot()
        assertThrows(IllegalStateException::class.java) {
            state.bindCurrentConfiguration(original.toBuilder().enabled(false).build(), RuntimeStorageUpdate.Replace(0L))
        }
        assertSame(before, state.configurationSnapshot())
    }

    private fun unusedStorage() = object : JankHunterBinaryStorage {
        override val fileSizeLimitBytes = Long.MAX_VALUE
        override val archivesSizeLimitBytes = Long.MAX_VALUE
        override fun openWriter(fileName: String): JankHunterBinaryWriter = error("Must not open")
        override fun createArtifact(fileName: String): JankHunterBinaryArtifact = error("Must not create")
        override fun cleanup(protectedPaths: Set<String>) = Unit
        override fun listFiles(): List<String> = emptyList()
    }

    @Test
    fun runtimeRequestAndBoundConfigArePublishedTogetherWithoutChangingBaseline() {
        val state = RuntimeState()
        state.bindInitialConfiguration(JankHunterConfig.builder().build())
        val initialization = state.configurationSnapshot().initializationGeneration
        state.bindRuntimeEnabled(false)
        val disabled = state.configurationSnapshot()
        assertFalse(disabled.desiredRuntimeEnabled)
        assertFalse(checkNotNull(disabled.config).runtimeEnabled())
        assertEquals(initialization, disabled.initializationGeneration)
        state.bindRuntimeEnabled(true)
        val enabled = state.configurationSnapshot()
        assertTrue(enabled.desiredRuntimeEnabled)
        assertTrue(checkNotNull(enabled.config).runtimeEnabled())
        assertTrue(enabled.lifecycleGeneration > disabled.lifecycleGeneration)
    }

    @Test
    fun explicitEnableNoopInvalidatesOldUpdaterButPreservesPendingStorageSwitch() {
        val state = RuntimeState()
        state.bindInitialConfiguration(JankHunterConfig.builder().build())
        val before = state.configurationSnapshot()
        state.bindRuntimeEnabled(true, invalidateSession = false)
        val after = state.configurationSnapshot()
        assertTrue(after.runtimeRequestGeneration > before.runtimeRequestGeneration)
        assertEquals(before.lifecycleGeneration, after.lifecycleGeneration)
        assertTrue(state.applyBinaryStorage(before.lifecycleGeneration, 1L, null))
        assertTrue(state.configurationSnapshot().desiredRuntimeEnabled)
    }
}
