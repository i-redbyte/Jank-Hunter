package io.jankhunter.runtime.internal.io

import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AsyncWriterControlCoordinatorTest {
    @Test
    fun asynchronousFlushWakesWorkerAfterAdmission() {
        var wakes = 0
        val coordinator = AsyncWriterControlCoordinator(
            quality = LogQualityCounters(),
            beginSubmission = { _, _ -> 0L },
            wakeWorker = { wakes++ },
        )

        coordinator.flush()

        assertEquals(1, wakes)
    }

    @Test
    fun exactControlSubmissionHonorsItsDeadline() {
        val completed = AtomicBoolean(false)
        val coordinator = AsyncWriterControlCoordinator(
            quality = LogQualityCounters(),
            beginSubmission = { _, _ -> 0L },
            wakeWorker = {},
        )
        val caller = Thread {
            completed.set(
                coordinator.submitBlocking(
                    timeoutMs = 10L,
                    writeLogGrowth = false,
                ),
            )
        }.apply {
            isDaemon = true
            start()
        }

        caller.join(1_000L)

        assertFalse("EXACT control ignored timeout", caller.isAlive)
        assertFalse(completed.get())
    }

    @Test
    fun timedOutPendingControlReleasesItsBoundedLaneSlot() {
        val coordinator = AsyncWriterControlCoordinator(
            quality = LogQualityCounters(),
            beginSubmission = { _, _ -> 0L },
            wakeWorker = {},
        )

        assertFalse(
            coordinator.submitBlocking(
                timeoutMs = 1L,
                writeLogGrowth = false,
            ),
        )

        assertFalse(coordinator.hasPending())
        assertTrue(
            coordinator.submitBlockingOutcome(
                timeoutMs = 1L,
                writeLogGrowth = false,
            ) == AsyncControlSubmissionOutcome.TIMED_OUT,
        )
    }

    @Test
    fun readyControlIsClaimedAndRemovedAsOneCoordinatorOperation() {
        val submitted = CountDownLatch(1)
        val outcome = AtomicReference<AsyncControlSubmissionOutcome>()
        val coordinator = AsyncWriterControlCoordinator(
            quality = LogQualityCounters(),
            beginSubmission = { _, _ -> 0L },
            wakeWorker = submitted::countDown,
        )
        val caller = Thread {
            outcome.set(
                coordinator.submitBlockingOutcome(
                    timeoutMs = 2_000L,
                    writeLogGrowth = false,
                ),
            )
        }.apply { start() }
        try {
            assertTrue("control was not submitted", submitted.await(1L, TimeUnit.SECONDS))

            val request = checkNotNull(coordinator.claimReady(completedSequence = 0L))
            assertFalse(coordinator.hasPending())
            request.complete(success = true)
            caller.join(1_000L)

            assertFalse("control caller remained blocked", caller.isAlive)
            assertEquals(AsyncControlSubmissionOutcome.SUCCEEDED, outcome.get())
        } finally {
            caller.interrupt()
            caller.join(1_000L)
        }
    }

    @Test
    fun storageSwitchFailsWhenAWorkerCannotBeStarted() {
        val coordinator = AsyncWriterControlCoordinator(
            quality = LogQualityCounters(),
            beginSubmission = { _, _ -> AsyncWriterControlCoordinator.NO_WORK },
            wakeWorker = {},
        )

        assertFalse(
            coordinator.submitBlocking(
                timeoutMs = 1L,
                writeLogGrowth = false,
                storageSwitch = StorageSwitchRequest(null),
            ),
        )
    }
}
