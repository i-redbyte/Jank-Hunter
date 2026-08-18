package io.jankhunter.runtime.internal.io

import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import io.jankhunter.runtime.JankHunterLogSnapshot
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class ProcessLogSnapshotCoordinatorArtTest {
    @Test
    fun concurrentRequestersCaptureEveryParticipantWithoutDeadlock() {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val directory = context.cacheDir.resolve("jankhunter-snapshot-${System.nanoTime()}").apply { mkdirs() }
        val first = ProcessLogSnapshotCoordinator.start(context, directory, "app") {
            JankHunterLogSnapshot(10L, listOf("/data/main.jhlog"))
        }
        val second = ProcessLogSnapshotCoordinator.start(context, directory, "app:remote") {
            JankHunterLogSnapshot(15L, listOf("/data/remote.jhlog"))
        }
        val start = CountDownLatch(1)
        val done = CountDownLatch(2)
        val firstResult = AtomicReference<JankHunterLogSnapshot?>()
        val secondResult = AtomicReference<JankHunterLogSnapshot?>()
        val firstThread = captureThread("JankHunterSnapshotRequesterMain", start, done, firstResult, first)
        val secondThread = captureThread("JankHunterSnapshotRequesterAgain", start, done, secondResult, first)
        try {
            assertEquals(2, ProcessSnapshotParticipants.active(directory).size)
            firstThread.start()
            secondThread.start()
            start.countDown()

            assertTrue("Concurrent snapshots timed out", done.await(30L, TimeUnit.SECONDS))
            assertSnapshot(firstResult.get())
            assertSnapshot(secondResult.get())
        } finally {
            first.close()
            second.close()
            firstThread.join(1_000L)
            secondThread.join(1_000L)
            assertFalse(firstThread.isAlive)
            assertFalse(secondThread.isAlive)
            directory.deleteRecursively()
        }
    }

    private fun captureThread(
        name: String,
        start: CountDownLatch,
        done: CountDownLatch,
        result: AtomicReference<JankHunterLogSnapshot?>,
        coordinator: ProcessLogSnapshotCoordinator,
    ): Thread = Thread(
        {
            try {
                start.await()
                result.set(coordinator.capture())
            } finally {
                done.countDown()
            }
        },
        name,
    )

    private fun assertSnapshot(snapshot: JankHunterLogSnapshot?) {
        assertNotNull(snapshot)
        requireNotNull(snapshot)
        assertEquals(listOf("/data/main.jhlog", "/data/remote.jhlog"), snapshot.logPaths)
        assertEquals(2, snapshot.processCount)
        assertEquals(5L, snapshot.captureSkewMs)
    }
}
