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

        handle.uninstall()

        assertFalse(fake.trackingEnabledState)
        assertEquals(1, fake.setTrackingEnabledCalls)
        registry.uninstallAll()
        assertEquals(1, fake.setTrackingEnabledCalls)
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
        registry.uninstallAll()
        assertEquals(1, first.setTrackingEnabledCalls)
        assertEquals(1, second.setTrackingEnabledCalls)
    }

    @Test
    fun weakRegistryDoesNotKeepManualHandleAlive() {
        val registry = JankHunterJankStats.HandleRegistry()
        val reference = registerAndDropHandle(registry)

        waitForGc(reference)

        assertNull("registry kept a manual JankStats handle strongly", reference.get())
        registry.uninstallAll()
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
