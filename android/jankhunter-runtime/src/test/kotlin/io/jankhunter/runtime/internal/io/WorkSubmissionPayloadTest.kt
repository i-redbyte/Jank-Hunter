package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertArrayEquals
import org.junit.Test

class WorkSubmissionPayloadTest {
    @Test fun scheduleSchemaPreservesDefaultWindowAndExplicitZeroDelay() {
        val payload = BinaryPayload()
        encodeWorkSubmission(payload, WorkSubmissionObservation(1, 2, 3, true, null, WorkSubmissionPolicy.NONE, 0,
            callerSource = "c", requestedPeriodMS = 30, requestedInitialDelayMS = 0))
        assertArrayEquals(byteArrayOf(4, 1, 2, 3, 2, 0, 0, 1, 99, 0, 0, 0, 5, 30, 0), payload.copyBytes())
    }

    @Test fun explicitBuilderHintsUseSchemaThreeWithoutClaimingAcceptance() {
        val payload = BinaryPayload()
        encodeWorkSubmission(payload, WorkSubmissionObservation(1, 2, 3, false, null, WorkSubmissionPolicy.NONE, 0,
            callerSource = "c", backoffPolicy = 2, requestedBackoffMS = 5, quotaPolicy = 1))
        assertArrayEquals(byteArrayOf(3, 1, 2, 3, 0, 0, 0, 1, 99, 2, 5, 1), payload.copyBytes())
    }

    @Test fun callerSourceIsVersionedAndHasExplicitTruncation() {
        val payload = BinaryPayload()
        encodeWorkSubmission(payload, WorkSubmissionObservation(1, 2, 3, false, null, WorkSubmissionPolicy.NONE, 0,
            callerSource = "c", sourceTruncated = true))
        assertArrayEquals(byteArrayOf(2, 1, 2, 3, 8, 0, 0, 1, 99), payload.copyBytes())
    }

    @Test(expected = IllegalStateException::class)
    fun type33CannotEnterFiveBitMicroPage() {
        ColumnarMicroPage().append(Jhlog.TYPE_WORK_SUBMISSION, 0, BinaryPayload(), null, null, 1)
    }

    @Test fun uniquePeriodicCallKeepsPolicyAndOmittedCount() {
        val payload = BinaryPayload()
        encodeWorkSubmission(payload, WorkSubmissionObservation(1, 2, 3, true, "s", WorkSubmissionPolicy.UPDATE, 4))
        assertArrayEquals(byteArrayOf(1, 1, 2, 3, 3, 4, 5, 1, 115), payload.copyBytes())
    }
    @Test(expected = IllegalArgumentException::class)
    fun periodicCallCannotUseAppendPolicy() {
        encodeWorkSubmission(BinaryPayload(), WorkSubmissionObservation(1, 2, 3, true, "s", WorkSubmissionPolicy.APPEND, 0))
    }
}
