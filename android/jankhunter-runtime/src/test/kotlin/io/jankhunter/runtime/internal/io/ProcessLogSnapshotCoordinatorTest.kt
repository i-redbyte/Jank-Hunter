package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterLogSnapshot
import java.io.IOException
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ProcessLogSnapshotCoordinatorTest {
    @Test
    fun responseCodecRoundTripsSuccessfulSnapshot() {
        val snapshot = JankHunterLogSnapshot(
            capturedAtMs = 42L,
            logPaths = listOf("/data/a.jhlog", "/data/process-remote.jhlog"),
            processCount = 2,
            captureSkewMs = 3L,
        )

        val decoded = ProcessLogSnapshotCoordinator.decodeResponse(
            ProcessLogSnapshotCoordinator.encodeResponse(snapshot),
        )

        assertTrue(decoded.succeeded)
        assertEquals(42L, decoded.capturedAtMs)
        assertEquals(snapshot.logPaths, decoded.paths)
    }

    @Test
    fun responseCodecRepresentsCaptureFailureExplicitly() {
        val decoded = ProcessLogSnapshotCoordinator.decodeResponse(
            ProcessLogSnapshotCoordinator.encodeResponse(null),
        )

        assertFalse(decoded.succeeded)
        assertEquals(0L, decoded.capturedAtMs)
        assertTrue(decoded.paths.isEmpty())
    }

    @Test
    fun responseCodecRejectsCorruption() {
        val encoded = ProcessLogSnapshotCoordinator.encodeResponse(
            JankHunterLogSnapshot(42L, listOf("/data/a.jhlog")),
        )
        encoded[encoded.lastIndex / 2] = (encoded[encoded.lastIndex / 2].toInt() xor 1).toByte()

        val failure = runCatching {
            ProcessLogSnapshotCoordinator.decodeResponse(encoded)
        }.exceptionOrNull()

        assertTrue(failure is IOException)
    }

    @Test
    fun deadlineRunnerReturnsFailureWithoutWaitingForHungCapture() {
        val started = CountDownLatch(1)
        val release = CountDownLatch(1)
        try {
            val deadlineNs = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(TEST_TIMEOUT_MS)
            val startedAtNs = System.nanoTime()

            val result = ProcessLogSnapshotCoordinator.runBeforeDeadline(deadlineNs) {
                started.countDown()
                while (release.count != 0L) {
                    try {
                        release.await()
                    } catch (_: InterruptedException) {
                        // Simulate capture code that does not cooperate with cancellation.
                    }
                }
                "late response"
            }
            val elapsedNs = System.nanoTime() - startedAtNs

            assertTrue(started.await(1L, TimeUnit.SECONDS))
            assertFalse(result.completed)
            assertNull(result.value)
            assertTrue(elapsedNs < TimeUnit.SECONDS.toNanos(MAX_TEST_DURATION_SECONDS))
        } finally {
            release.countDown()
        }
    }

    @Test
    fun deadlineRunnerReturnsCompletedCapture() {
        val deadlineNs = System.nanoTime() + TimeUnit.SECONDS.toNanos(1L)

        val result = ProcessLogSnapshotCoordinator.runBeforeDeadline(deadlineNs) { "snapshot" }

        assertTrue(result.completed)
        assertEquals("snapshot", result.value)
    }

    private companion object {
        const val TEST_TIMEOUT_MS = 100L
        const val MAX_TEST_DURATION_SECONDS = 2L
    }
}
