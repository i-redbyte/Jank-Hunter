package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterLogSnapshot
import java.io.IOException
import java.io.RandomAccessFile
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
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

    @Test
    fun deadlineRunnerDoesNotHideFatalCaptureFailure() {
        assertThrows(FatalCaptureError::class.java) {
            ProcessLogSnapshotCoordinator.runBeforeDeadline(Long.MAX_VALUE) {
                throw FatalCaptureError()
            }
        }
    }

    @Test
    fun fileLockAcquisitionHonorsTheCaptureDeadline() {
        val file = Files.createTempFile("jankhunter-snapshot-lock", ".lock").toFile()
        RandomAccessFile(file, "rw").use { owner ->
            RandomAccessFile(file, "rw").use { contender ->
                owner.channel.lock().use {
                    val startedAtNs = System.nanoTime()
                    val deadlineNs = startedAtNs + TimeUnit.MILLISECONDS.toNanos(TEST_TIMEOUT_MS)

                    val acquired = ProcessLogSnapshotCoordinator.acquireLockBeforeDeadline(
                        contender.channel,
                        deadlineNs,
                    )
                    val elapsedNs = System.nanoTime() - startedAtNs

                    assertNull(acquired)
                    assertTrue(elapsedNs >= TimeUnit.MILLISECONDS.toNanos(TEST_TIMEOUT_MS / 2L))
                    assertTrue(elapsedNs < TimeUnit.SECONDS.toNanos(MAX_TEST_DURATION_SECONDS))
                }
            }
        }
    }

    @Test
    fun responseAdmissionIsReleasedWhenCaptureWasNotHandedOff() {
        val responseInFlight = AtomicBoolean(true)

        ProcessLogSnapshotCoordinator.releaseUnhandedResponseAdmission(
            responseInFlight = responseInFlight,
            captureAllowed = true,
            captureHandedOff = false,
        )

        assertFalse(responseInFlight.get())
    }

    @Test
    fun responseAdmissionStaysOwnedByHandedOffCapture() {
        val responseInFlight = AtomicBoolean(true)

        ProcessLogSnapshotCoordinator.releaseUnhandedResponseAdmission(
            responseInFlight = responseInFlight,
            captureAllowed = true,
            captureHandedOff = true,
        )

        assertTrue(responseInFlight.get())
    }

    private companion object {
        const val TEST_TIMEOUT_MS = 100L
        const val MAX_TEST_DURATION_SECONDS = 2L
    }

    private class FatalCaptureError : VirtualMachineError()
}
