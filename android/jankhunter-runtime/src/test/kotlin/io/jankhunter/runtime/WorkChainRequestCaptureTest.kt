package io.jankhunter.runtime

import androidx.work.OneTimeWorkRequest
import androidx.work.Worker
import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Assert.assertThrows
import org.junit.Test

class WorkChainRequestCaptureTest {
    @Test fun opaqueSubmissionListIsNeverEvaluatedOnTheProducer() {
        var touches = 0
        val requests = object : AbstractList<Any>() {
            override val size: Int get() { touches++; error("opaque size callback") }
            override fun get(index: Int): Any { touches++; error("opaque get callback") }
        }
        var failures = 0
        var observed = 0
        WorkChainRequestCapture.captureRequests(requests, 128, {}, { failures++ }) { _, _, _ -> observed++ }
        assertEquals(0, touches)
        assertEquals(0, observed)
        assertEquals(1, failures)
    }

    @Test fun materializedSubmissionListReadsOnlyFirst128AndCountsOmissions() {
        val request = OneTimeWorkRequest.Builder(Worker::class.java).build()
        val requests = ArrayList<Any>(129).apply { repeat(129) { add(request) } }
        var observed = 0
        var omitted = 0
        WorkChainRequestCapture.captureRequests(requests, 128, { omitted += it }, { throw it }) { item, _, count ->
            assertSame(request, item)
            assertEquals(129, count)
            observed++
        }
        assertEquals(128, observed)
        assertEquals(1, omitted)
    }

    @Test fun fatalObservationFailureIsNeverDowngradedToMetadataLoss() {
        val request = OneTimeWorkRequest.Builder(Worker::class.java).build()
        var failures = 0
        assertThrows(TestVmFailure::class.java) {
            WorkChainRequestCapture.captureRequests(arrayListOf(request), 8, {}, { failures++ }) { _, _, _ ->
                throw TestVmFailure()
            }
        }
        assertEquals(0, failures)
    }

    class TestVmFailure : VirtualMachineError()

    @Test fun failedMetadataForOneRequestDoesNotLoseLaterRequests() {
        val request = OneTimeWorkRequest.Builder(Worker::class.java).build()
        val requests = arrayListOf<Any?>(Any(), request, null)
        var failures = 0
        var observed = 0
        WorkChainRequestCapture.captureRequests(requests, 8, {}, { failures++ }) { item, _, _ ->
            assertSame(request, item)
            observed++
        }
        assertEquals(2, failures)
        assertEquals(1, observed)
    }
}
