package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import org.junit.Assert.assertTrue
import org.junit.Assert.assertFalse
import org.junit.Test

class RuntimeWriterEpochBoundaryTest {
    @Test
    fun concurrentCloseCannotPublishStopBeforeTheEpochSnapshotFinishes() {
        val directory = Files.createTempDirectory("jankhunter-epoch-close").toFile()
        val writer = AsyncLogWriterFactory().open(directory, JankHunterConfig.builder().build(), "main")
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val secondFinished = CountDownLatch(1)
        val secondResult = AtomicBoolean(true)
        writer.bindCollectionEndObserver {
            entered.countDown()
            check(release.await(5L, TimeUnit.SECONDS))
        }
        writer.counter("test", 1L)
        assertTrue(writer.flushBlocking())
        val first = Thread { writer.close() }
        val second = Thread { secondResult.set(writer.close(25L)); secondFinished.countDown() }
        try {
            first.start()
            assertTrue(entered.await(1L, TimeUnit.SECONDS))
            second.start()
            assertTrue("concurrent close exceeded its deadline", secondFinished.await(1L, TimeUnit.SECONDS))
            assertFalse("close claimed completion before the epoch snapshot", secondResult.get())
            assertTrue("consumer could seal before epoch quality was recorded", writer.isAcceptingEvents())
        } finally {
            release.countDown()
            first.join(3_000L)
            second.join(3_000L)
            writer.close()
            directory.deleteRecursively()
        }
        assertTrue(!first.isAlive && !second.isAlive)
    }
}
