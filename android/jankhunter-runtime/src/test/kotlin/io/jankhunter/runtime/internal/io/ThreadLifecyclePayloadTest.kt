package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertArrayEquals
import org.junit.Test

class ThreadLifecyclePayloadTest {
    @Test fun runPointPreservesJavaAndLinuxIdentityWithoutInventingStart() {
        val payload = BinaryPayload()
        encodeThreadLifecycle(payload, ThreadLifecycleObservation(ThreadLifecycleStage.RUN_OBSERVED, 9, 42, 123, true, "n", "s", ""))
        assertArrayEquals(byteArrayOf(1, 1, 9, 42, 123, 1, 1, 110, 1, 115, 0), payload.copyBytes())
    }

    @Test(expected = IllegalArgumentException::class)
    fun startReturnCannotClaimTargetKernelTid() {
        encodeThreadLifecycle(BinaryPayload(), ThreadLifecycleObservation(ThreadLifecycleStage.START_RETURNED, 9, 42, 123, true, "n", "s", ""))
    }
}
