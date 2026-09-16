package io.jankhunter.workmanager

import android.content.Context
import androidx.test.platform.app.InstrumentationRegistry
import androidx.work.ExistingWorkPolicy
import androidx.work.OneTimeWorkRequest
import androidx.work.WorkInfo
import androidx.work.Operation
import com.google.common.util.concurrent.ListenableFuture
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executor
import org.junit.Assert.assertTrue
import androidx.work.WorkManager
import androidx.work.Worker
import androidx.work.WorkerParameters
import java.util.UUID
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertNotNull
import org.junit.Test

class WorkEnqueueAcceptanceArtTest {
    @Test
    fun keepWithExistingWorkDoesNotPublishTheIgnoredUUID() {
        val manager = WorkManager.getInstance(InstrumentationRegistry.getInstrumentation().targetContext)
        val uniqueName = "jh-keep-" + UUID.randomUUID()
        val existing = request()
        val ignored = request()
        try {
            manager.enqueueUniqueWork(uniqueName, ExistingWorkPolicy.KEEP, existing).result.get(5L, TimeUnit.SECONDS)
            val operation = manager.enqueueUniqueWork(uniqueName, ExistingWorkPolicy.KEEP, ignored)
            operation.result.get(5L, TimeUnit.SECONDS)
            assertNotNull(manager.getWorkInfoById(existing.id).get(5L, TimeUnit.SECONDS))
            assertNull("KEEP must really ignore the new UUID", manager.getWorkInfoById(ignored.id).get(5L, TimeUnit.SECONDS))
            assertEquals("successful operation does not mean this UUID was registered", 0, observe(manager, operation, ignored))
        } finally {
            manager.cancelUniqueWork(uniqueName).result.get(5L, TimeUnit.SECONDS)
        }
    }

    @Test
    fun keepWithoutExistingWorkPublishesTheRegisteredUUID() {
        val manager = WorkManager.getInstance(InstrumentationRegistry.getInstrumentation().targetContext)
        val uniqueName = "jh-keep-new-" + UUID.randomUUID()
        val request = request()
        try {
            val operation = manager.enqueueUniqueWork(uniqueName, ExistingWorkPolicy.KEEP, request)
            operation.result.get(5L, TimeUnit.SECONDS)
            assertNotNull(manager.getWorkInfoById(request.id).get(5L, TimeUnit.SECONDS))
            assertEquals(1, observe(manager, operation, request))
        } finally {
            manager.cancelUniqueWork(uniqueName).result.get(5L, TimeUnit.SECONDS)
        }
    }

    @Test
    fun replaceAndAppendObserveOnlyTheRequestedUUID() {
        val manager = WorkManager.getInstance(InstrumentationRegistry.getInstrumentation().targetContext)
        for (policy in listOf(ExistingWorkPolicy.REPLACE, ExistingWorkPolicy.APPEND)) {
            val uniqueName = "jh-policy-" + UUID.randomUUID()
            try {
                val existing = request()
                manager.enqueueUniqueWork(uniqueName, ExistingWorkPolicy.KEEP, existing).result.get(5L, TimeUnit.SECONDS)
                val next = request()
                val operation = manager.enqueueUniqueWork(uniqueName, policy, next)
                operation.result.get(5L, TimeUnit.SECONDS)
                assertEquals(1, observe(manager, operation, next))
                val old = manager.getWorkInfoById(existing.id).get(5L, TimeUnit.SECONDS)
                val current = manager.getWorkInfoById(next.id).get(5L, TimeUnit.SECONDS)
                assertNotNull(current)
                if (policy == ExistingWorkPolicy.REPLACE) {
                    assertTrue(old == null || old.state == WorkInfo.State.CANCELLED)
                } else {
                    assertEquals(WorkInfo.State.BLOCKED, current!!.state)
                }
            } finally {
                manager.cancelUniqueWork(uniqueName).result.get(5L, TimeUnit.SECONDS)
            }
        }
    }

    private fun observe(manager: WorkManager, operation: Operation, request: OneTimeWorkRequest): Int {
        val observed = AtomicInteger()
        val done = CountDownLatch(1)
        val lookup = WorkRegistrationLookup { id ->
            val query = manager.getWorkInfoById(id)
            object : ListenableFuture<WorkInfo?> by query {
                override fun addListener(listener: Runnable, executor: Executor) {
                    query.addListener({
                        try { listener.run() } finally { done.countDown() }
                    }, executor)
                }
            }
        }
        WorkEnqueueListener(operation.result, WorkEnqueueBatch.capture(request), WorkerEnqueueSink { _, _ ->
            observed.incrementAndGet()
        }, lookup).run()
        assertTrue("asynchronous observation did not finish", done.await(5L, TimeUnit.SECONDS))
        return observed.get()
    }

    private fun request(): OneTimeWorkRequest = OneTimeWorkRequest.Builder(DelayedWorker::class.java)
        .setInitialDelay(1L, TimeUnit.DAYS).build()

    class DelayedWorker(context: Context, parameters: WorkerParameters) : Worker(context, parameters) {
        override fun doWork(): Result = Result.success()
    }
}
