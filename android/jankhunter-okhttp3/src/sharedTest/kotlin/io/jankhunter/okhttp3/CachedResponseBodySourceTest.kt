package io.jankhunter.okhttp3

import io.jankhunter.runtime.JankHunterHttpEvent
import java.io.IOException
import java.util.concurrent.CyclicBarrier
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import okhttp3.Response
import okio.Buffer
import okio.Source
import okio.Timeout
import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class CachedResponseBodySourceTest {
    @Test
    fun onlySelectedBodyNotifiesOnceWhetherItWasClosedBeforeOrAfterBinding() {
        for (early in listOf(true, false)) {
            val source = CachedResponseBodySource(Buffer().writeUtf8("x"))
            val observer = Observation()
            if (early) source.close()
            assertEquals(0, observer.count.get())
            source.bind(observer)
            source.close()
            source.close()
            source.bind(observer)
            assertEquals(1, observer.count.get())
            assertEquals(JankHunterHttpEvent.FAILURE_KIND_UNKNOWN, observer.failure)
            assertReleased(source)
        }
    }

    @Test
    fun sourceIOExceptionIsPreservedAndFailureDoesNotRetainTheThrowable() {
        val original = IOException("cache read failed")
        val source = CachedResponseBodySource(object : Source {
            override fun read(sink: Buffer, byteCount: Long): Long = throw original
            override fun close() = Unit
            override fun timeout(): Timeout = Timeout.NONE
        })
        try { source.read(Buffer(), 1L); error("missing I/O exception") } catch (failure: IOException) { assertSame(original, failure) }
        val observer = Observation()
        source.bind(observer)
        source.close()
        assertEquals(1, observer.count.get())
        assertEquals(JankHunterHttpEvent.FAILURE_KIND_IO, observer.failure)
        assertReleased(source)
        assertTrue(source.javaClass.declaredFields.none { Throwable::class.java.isAssignableFrom(it.type) })
    }

    @Test
    fun detachedCallDoesNotRemainReferencedByTheCachedBody() {
        val source = CachedResponseBodySource(Buffer())
        val observer = Observation()
        source.bind(observer)
        source.detach(observer)
        source.close()
        assertEquals(0, observer.count.get())
        assertReleased(source)
    }

    @Test
    fun concurrentSelectionAndCompletionPublishExactlyOnce() {
        val worker = Executors.newSingleThreadExecutor()
        try {
            repeat(200) {
                val source = CachedResponseBodySource(Buffer())
                val observer = Observation()
                val start = CyclicBarrier(2)
                val closed = worker.submit { start.await(5L, TimeUnit.SECONDS); source.close() }
                start.await(5L, TimeUnit.SECONDS)
                source.bind(observer)
                closed.get(5L, TimeUnit.SECONDS)
                assertEquals(1, observer.count.get())
                assertReleased(source)
            }
        } finally {
            worker.shutdownNow()
            assertTrue(worker.awaitTermination(5L, TimeUnit.SECONDS))
        }
    }

    private fun assertReleased(source: CachedResponseBodySource) {
        val field = source.javaClass.getDeclaredField("observer").apply { isAccessible = true }
        assertEquals(null, field.get(source))
    }

    private class Observation : HttpTransportObservation {
        val count = AtomicInteger()
        var failure = -1
        override val firstByteClock = NetworkLongSource { 0L }
        override fun onFirstByte(atMs: Long) = Unit
        override fun armTransport(source: HttpResponseByteSource, streamId: Int) = Unit
        override fun onFinalResponse(response: Response) = Unit
        override fun onCachedBodyComplete(failureKind: Int) { failure = failureKind; count.incrementAndGet() }
    }
}
