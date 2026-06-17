package io.jankhunter.runtime.integration

import java.lang.ref.WeakReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Test

class JankHunterJankStatsTest {
    @Test
    fun uninstallRemovesLiveHandleFromWeakRegistry() {
        val registry = JankHunterJankStats.HandleRegistry()
        val fake = FakeJankStats()
        val handle = JankHunterJankStats.Handle(fake, registry)

        registry.add(handle)
        assertEquals(1, registry.liveCount())

        handle.uninstall()

        assertFalse(fake.trackingEnabledState)
        assertEquals(1, fake.setTrackingEnabledCalls)
        assertEquals(0, registry.liveCount())
    }

    @Test
    fun uninstallAllClearsRegistryAndUninstallsLiveHandles() {
        val registry = JankHunterJankStats.HandleRegistry()
        val first = FakeJankStats()
        val second = FakeJankStats()
        registry.add(JankHunterJankStats.Handle(first, registry))
        registry.add(JankHunterJankStats.Handle(second, registry))

        registry.uninstallAll()

        assertFalse(first.trackingEnabledState)
        assertFalse(second.trackingEnabledState)
        assertEquals(0, registry.liveCount())
    }

    @Test
    fun weakRegistryDoesNotKeepManualHandleAlive() {
        val registry = JankHunterJankStats.HandleRegistry()
        val reference = registerAndDropHandle(registry)

        waitForGc(reference)

        assertNull("registry kept a manual JankStats handle strongly", reference.get())
        assertEquals(0, registry.liveCount())
    }

    private fun registerAndDropHandle(
        registry: JankHunterJankStats.HandleRegistry,
    ): WeakReference<JankHunterJankStats.Handle> {
        val handle = JankHunterJankStats.Handle(FakeJankStats(), registry)
        registry.add(handle)
        return WeakReference(handle)
    }

    private fun waitForGc(reference: WeakReference<*>) {
        repeat(40) {
            if (reference.get() == null) return
            System.gc()
            System.runFinalization()
            Thread.sleep(10)
        }
    }

    class FakeJankStats {
        var trackingEnabledState = true
        var setTrackingEnabledCalls = 0

        fun setTrackingEnabled(enabled: Boolean) {
            trackingEnabledState = enabled
            setTrackingEnabledCalls++
        }
    }
}
