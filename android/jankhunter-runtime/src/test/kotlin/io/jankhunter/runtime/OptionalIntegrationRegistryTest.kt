package io.jankhunter.runtime

import android.content.Context
import android.content.ContextWrapper
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class OptionalIntegrationRegistryTest {
    @Test
    fun parsesOnlyBoundedValidUniqueEntrypoints() {
        val parsed = OptionalIntegrationRegistry.parseEntrypoints(
            " io.jankhunter.One,invalid-name,io.jankhunter.One,io.jankhunter.Two ",
        )

        assertEquals(listOf("io.jankhunter.One", "io.jankhunter.Two"), parsed)
        assertTrue(OptionalIntegrationRegistry.parseEntrypoints(null).isEmpty())
    }

    @Test
    fun startsDispatchesAndStopsFailOpen() {
        val first = FakeIntegration("first")
        val failing = FakeIntegration("failing", failContext = true)
        val diagnostics = mutableListOf<String>()
        val registry = OptionalIntegrationRegistry(
            discover = { listOf(first, failing) },
            diagnostic = diagnostics::add,
        )
        val context: Context = ContextWrapper(null)
        val snapshot = JankHunterContextSnapshot("Feed", "Images", "scroll", "bind")

        registry.startAll(context)
        registry.startAll(context)
        registry.onContextChanged(Thread.currentThread(), "Feed", "Images", "scroll", "bind")
        registry.onMainThreadStall(Thread.currentThread(), snapshot)
        registry.stopAll(100L)

        assertEquals(1, first.starts)
        assertEquals(1, first.contexts)
        assertEquals(1, first.stalls)
        assertEquals(1, first.stops)
        assertEquals(0, registry.activeCount())
        assertTrue(diagnostics.any { it == "failing.context_failed" })
    }

    private class FakeIntegration(
        override val id: String,
        private val failContext: Boolean = false,
    ) : JankHunterRuntimeIntegration {
        var starts = 0
        var stops = 0
        var contexts = 0
        var stalls = 0

        override fun start(context: Context) {
            starts++
        }

        override fun stop(timeoutMs: Long) {
            stops++
        }

        override fun onContextChanged(
            thread: Thread,
            screen: String?,
            owner: String?,
            flow: String?,
            step: String?,
        ) {
            contexts++
            if (failContext) error("expected")
        }

        override fun onMainThreadStall(thread: Thread, context: JankHunterContextSnapshot) {
            stalls++
        }
    }
}
