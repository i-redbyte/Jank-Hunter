package io.jankhunter.runtime

import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class BoundedWeakIdentityCacheTest {
    @Test
    fun metadataCreationDoesNotHoldTheSharedCacheMonitor() {
        val cache = BoundedWeakIdentityCache<Any, Any>(2)
        val value = Any()
        assertSame(value, cache.getOrPut(Any()) {
            assertFalse("Reflection and class loading must run outside the shared cache lock", Thread.holdsLock(cache))
            value
        })
    }

    @Test
    fun stalledClassResolutionCannotBlockAnotherCachedType() {
        val cache = BoundedWeakIdentityCache<Any, Any>(2)
        val cachedKey = Any()
        val cachedValue = Any()
        cache.getOrPut(cachedKey) { cachedValue }
        val resolving = CountDownLatch(1)
        val releaseResolution = CountDownLatch(1)
        val hitFinished = CountDownLatch(1)
        val failure = AtomicReference<Throwable?>()
        val resolver = Thread {
            try {
                cache.getOrPut(Any()) {
                    resolving.countDown()
                    check(releaseResolution.await(5, TimeUnit.SECONDS))
                    Any()
                }
            } catch (error: Throwable) { failure.compareAndSet(null, error) }
        }
        val reader = Thread {
            try {
                assertSame(cachedValue, cache.getOrPut(cachedKey) { error("Cache hit recomputed metadata") })
            } catch (error: Throwable) { failure.compareAndSet(null, error) }
            finally { hitFinished.countDown() }
        }
        try {
            resolver.start()
            assertTrue(resolving.await(2, TimeUnit.SECONDS))
            reader.start()
            assertTrue("Unrelated cache hit waited for class resolution", hitFinished.await(1, TimeUnit.SECONDS))
        } finally {
            releaseResolution.countDown()
            resolver.join(2_000)
            reader.join(2_000)
        }
        assertFalse(resolver.isAlive)
        assertFalse(reader.isAlive)
        failure.get()?.let { throw AssertionError("cache operation failed", it) }
    }

    @Test
    fun aPublicationMadeWhileTheFactoryRunsWinsOverItsResult() {
        val cache = BoundedWeakIdentityCache<Any, Any>(2)
        val key = Any()
        val winner = Any()
        assertSame(winner, cache.getOrPut(key) {
            cache.getOrPut(key) { winner }
            Any()
        })
        assertSame(winner, cache.getOrPut(key) { error("Published metadata was lost") })
    }
}
