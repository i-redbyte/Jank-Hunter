package io.jankhunter.workmanager

import android.content.Context
import androidx.work.OneTimeWorkRequest
import androidx.work.WorkRequest
import androidx.work.Worker
import androidx.work.WorkerParameters
import org.junit.Assert.assertEquals
import org.junit.Test

class WorkEnqueueBudgetTest {
    @Test fun hugeInputDoesNotAllocateOrInspectBeyondCaptureBudget() {
        val request = OneTimeWorkRequest.Builder(TestWorker::class.java).build()
        var reads = 0
        val requests = object : AbstractList<WorkRequest>() {
            override val size: Int = Int.MAX_VALUE
            override fun get(index: Int): WorkRequest { reads++; return request }
        }
        val batch = WorkEnqueueBatch.capture(requests)
        assertEquals(128, reads)
        assertEquals(128, batch.size)
        assertEquals(Int.MAX_VALUE - 128, batch.omittedRequests)
    }
    private class TestWorker(context: Context, parameters: WorkerParameters) : Worker(context, parameters) {
        override fun doWork(): Result = Result.success()
    }
}
