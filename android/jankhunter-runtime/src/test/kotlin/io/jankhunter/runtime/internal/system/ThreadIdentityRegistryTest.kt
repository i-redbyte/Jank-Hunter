package io.jankhunter.runtime.internal.system

import java.lang.ref.WeakReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertNotNull
import org.junit.Test

class ThreadIdentityRegistryTest {
    @Test fun registrationNeverWaitsForTheSamplerRegistryOwner() {
        val entered = java.util.concurrent.CountDownLatch(1)
        val release = java.util.concurrent.CountDownLatch(1)
        val done = java.util.concurrent.CountDownLatch(1)
        val failure = java.util.concurrent.atomic.AtomicReference<Throwable?>()
        val registry = ThreadIdentityRegistry(weak = {
            entered.countDown()
            check(release.await(5, java.util.concurrent.TimeUnit.SECONDS))
            WeakReference(it)
        })
        val holder = Thread { registry.register(1L, Thread.currentThread(), 1L, "holder") }
        val producer = Thread {
            try { org.junit.Assert.assertFalse(registry.register(2L, Thread.currentThread(), 2L, "contended")) }
            catch (error: Throwable) { failure.set(error) }
            finally { done.countDown() }
        }
        try {
            holder.start()
            org.junit.Assert.assertTrue(entered.await(5, java.util.concurrent.TimeUnit.SECONDS))
            producer.start()
            org.junit.Assert.assertTrue("identity producer waited for sampler owner", done.await(100, java.util.concurrent.TimeUnit.MILLISECONDS))
            assertEquals(null, failure.get())
        } finally { release.countDown(); holder.join(5_000L); producer.join(5_000L) }
    }

    @Test fun failingApplicationThreadStateDoesNotDiscardKernelSnapshot() {
        val entered = java.util.concurrent.CountDownLatch(1)
        val release = java.util.concurrent.CountDownLatch(1)
        val thread = object : Thread() {
            override fun getState(): State = error("application state getter failed")
            override fun run() { entered.countDown(); release.await() }
        }
        thread.start()
        try {
            org.junit.Assert.assertTrue(entered.await(5, java.util.concurrent.TimeUnit.SECONDS))
            val registry = ThreadIdentityRegistry()
            registry.register(42, thread, 1, "run")
            assertNull(registry.observe(42, 2))
        } finally {
            release.countDown()
            thread.join(5000)
            org.junit.Assert.assertFalse(thread.isAlive)
        }
    }

    @Test fun onlyLinksRegistrationBeforeScanToStillLivingJavaThread() {
        val registry = ThreadIdentityRegistry()
        val thread = Thread.currentThread()
        registry.register(42, thread, 100, "app.Task.run")
        assertNull(registry.observe(42, 99))
        val linked = checkNotNull(registry.observe(42, 101))
        assertEquals(thread.id, linked.id)
        assertEquals(thread.state, linked.state)
        assertEquals("app.Task.run", linked.source)
        assertNull(registry.observe(43, 101))
        registry.register(42, Thread(), 102, "new generation")
        assertNull(registry.observe(42, 103))
    }

    @Test fun boundsWeakEntriesAndReleasesClearedEntriesOnMaintenance() {
        val references = mutableListOf<WeakReference<Thread>>()
        val registry = ThreadIdentityRegistry(limit = 1, weak = { WeakReference(it).also(references::add) })
        registry.register(1, Thread.currentThread(), 1, "first")
        registry.register(2, Thread.currentThread(), 1, "second")
        assertEquals(1L, registry.dropped)
        assertNull(registry.observe(2, 2))
        references.single().clear()
        registry.prune()
        registry.register(2, Thread.currentThread(), 3, "second")
        assertNotNull(registry.observe(2, 4))
        assertNull(registry.observe(1, 4))
    }

    @Test fun separateEpochRegistriesNeverShareJavaIdentity() {
        val old = ThreadIdentityRegistry()
        old.register(7, Thread.currentThread(), 1, "old")
        assertNull(ThreadIdentityRegistry().observe(7, 2))
        assertNotNull(old.observe(7, 2))
    }

    @Test fun ownershipComesOnlyFromExplicitExecutingThreadRegistration() {
        val registry = ThreadIdentityRegistry()
        registry.register(7, Thread.currentThread(), 1, "JankHunterMaintenance")
        assertEquals(false, registry.observe(7, 2)?.ownedByJankHunter)
        registry.register(8, Thread.currentThread(), 1, "internal maintenance", ownedByJankHunter = true)
        assertEquals(true, registry.observe(8, 2)?.ownedByJankHunter)
        assertNull(registry.observe(8, 0))
    }

    @Test fun capacityFailureIsReportedAndSameThreadCanBeUpgradedWithoutTIDGuessing() {
        val registry = ThreadIdentityRegistry(limit = 1)
        val current = Thread.currentThread()
        assertEquals(true, registry.register(1, current, 1, "app.run"))
        assertEquals(false, registry.register(2, current, 2, "owned", ownedByJankHunter = true))
        assertEquals(true, registry.register(1, current, 3, "owned", ownedByJankHunter = true))
        assertEquals(true, registry.observe(1, 4)?.ownedByJankHunter)
        assertEquals("owned", registry.observe(1, 4)?.source)
        assertNull(registry.observe(1, 2))
    }
}
