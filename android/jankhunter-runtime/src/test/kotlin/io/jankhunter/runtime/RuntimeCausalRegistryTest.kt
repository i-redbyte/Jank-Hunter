package io.jankhunter.runtime

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.locks.ReentrantLock

class RuntimeCausalRegistryTest {
    @Test
    fun contendedIdentityShardNeverBlocksAnApplicationProducer() {
        val registry = RuntimeCausalRegistry(capacity = 4, hash = { 0 })
        val identity = Any()
        val id = registry.register(2, identity)
        assertTrue(id > 0L)
        val shardsField = registry.javaClass.getDeclaredField("shards").apply { isAccessible = true }
        val shard = (shardsField.get(registry) as Array<*>)[0]!!
        val acquired = CountDownLatch(1)
        val release = CountDownLatch(1)
        val done = CountDownLatch(1)
        val failure = java.util.concurrent.atomic.AtomicReference<Throwable?>()
        // This same regression runs against the historical monitor implementation and tryLock.
        val lock = try {
            shard.javaClass.getDeclaredField("lock").apply { isAccessible = true }.get(shard) as ReentrantLock
        } catch (missing: NoSuchFieldException) { null }
        val holder = Thread {
            if (lock == null) synchronized(shard) { acquired.countDown(); release.await() }
            else {
                lock.lock()
                try { acquired.countDown(); release.await() } finally { lock.unlock() }
            }
        }
        val producer = Thread {
            try {
                assertEquals(0L, registry.find(2, identity))
                assertEquals(0L, registry.register(2, Any()))
                assertFalse(registry.bind(2, Any(), id))
            } catch (error: Throwable) { failure.set(error) }
            finally { done.countDown() }
        }
        try {
            holder.start()
            assertTrue(acquired.await(5L, TimeUnit.SECONDS))
            producer.start()
            assertTrue("identity lookup waited for a foreign shard lock", done.await(100L, TimeUnit.MILLISECONDS))
            assertEquals(null, failure.get())
        } finally {
            release.countDown()
            holder.join(5_000L)
            producer.join(5_000L)
            registry.close()
        }
    }

    @Test
    fun configuredCapacityHasAConservativeStructuralBudgetBelowSixteenMiB() {
        val registry = RuntimeCausalRegistry(capacity = 4096)
        val retained = ArrayList<Any>(4096)
        repeat(50_000) {
            val identity = Any()
            if (registry.register(2, identity) > 0L) retained.add(identity)
        }
        assertTrue(retained.isNotEmpty())
        assertTrue(registry.liveEntries() <= 4096)
        // Structural allowance for weak entries, table references and 16 shard/queue objects.
        // This is not a measured heap size and excludes application-owned referent objects.
        val estimatedBytes = 4096L * 512L + registry.allocatedSlotsForTest() * 8L + 16L * 4096L
        assertTrue("conservative entry/table/shard estimate = $estimatedBytes", estimatedBytes < 16L * 1024L * 1024L)
        registry.close()
        assertEquals(0, registry.liveEntries())
    }

    @Test
    fun failedStartDoesNotMakeManualRunLookLikeThreadRoot() {
        val registry = RuntimeCausalRegistry(capacity = 4)
        val thread = Thread { }
        assertTrue(registry.register(1, thread) > 0L)
        assertTrue(registry.markThreadStart(thread))
        assertTrue(registry.hasThreadStart(thread))
        registry.markThreadStartResult(thread, returned = false)
        assertFalse(registry.hasThreadStart(thread))

        // A concurrent second failed start cannot erase an earlier successful start.
        registry.markThreadStart(thread)
        registry.markThreadStart(thread)
        registry.markThreadStartResult(thread, returned = true)
        registry.markThreadStartResult(thread, returned = false)
        assertTrue(registry.hasThreadStart(thread))
    }

    @Test
    fun collisionsAndCollectedEntriesDoNotBindTheNextObjectToTheOldId() {
        val registry = RuntimeCausalRegistry(capacity = 2, hash = { 7 })
        val first = Any()
        val second = Any()
        val firstId = registry.register(1, first)
        val secondId = registry.register(1, second)

        assertTrue(firstId > 0L)
        assertTrue(secondId > firstId)
        assertEquals(firstId, registry.find(1, first))
        assertEquals(secondId, registry.find(1, second))
        assertEquals(0L, registry.register(1, Any()))

        assertTrue(registry.clearAndEnqueueForTest(1, first))
        val replacement = Any()
        val replacementId = registry.register(1, replacement)
        assertTrue(replacementId > secondId)
        assertEquals(0L, registry.find(1, first))
        assertEquals(secondId, registry.find(1, second))
        assertEquals(replacementId, registry.find(1, replacement))
    }

    @Test
    fun closeRejectsOldBindingsAndConcurrentRegistrationsNeverDuplicateAnIdentity() {
        val registry = RuntimeCausalRegistry(capacity = 64)
        val shared = Any()
        val start = CountDownLatch(1)
        val pool = Executors.newFixedThreadPool(8)
        try {
            val futures = (1..8).map {
                pool.submit<Long> {
                    start.await()
                    registry.register(2, shared)
                }
            }
            start.countDown()
            val ids = futures.map { it.get(5, TimeUnit.SECONDS) }.filter { it > 0L }.toSet()
            // Contended attempts may be absent; admitted attempts must never duplicate identity.
            assertEquals(1, ids.size)
            assertTrue(ids.single() > 0L)
            assertEquals(1, registry.liveEntries())

            registry.close()
            assertEquals(0L, registry.find(2, shared))
            assertEquals(0L, registry.register(2, Any()))
            assertFalse(registry.bind(2, Any(), ids.single()))
        } finally {
            pool.shutdownNow()
        }
    }
}
