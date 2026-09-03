package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertFalse
import org.junit.Test

class AsyncWriterControlCoordinatorTest {
    @Test
    fun storageSwitchFailsWhenAWorkerCannotBeStarted() {
        val coordinator = AsyncWriterControlCoordinator(
            exactEventCollection = false,
            quality = LogQualityCounters(),
            beginSubmission = { AsyncWriterControlCoordinator.NO_WORK },
            wakeWorker = {},
        )

        assertFalse(
            coordinator.submitBlocking(
                timeoutMs = 1L,
                writeLogGrowth = false,
                waitForExactFrontier = true,
                storageSwitch = StorageSwitchRequest(null),
            ),
        )
    }
}
