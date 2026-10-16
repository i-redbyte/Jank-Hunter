package io.jankhunter.workmanager

import androidx.test.platform.app.InstrumentationRegistry
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.PeriodicWorkRequest
import androidx.work.WorkManager
import io.jankhunter.runtime.JankHunterWorkRegistryResult
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test

class WorkScheduleRegistryArtTest {
    @Test fun declaredParametersMatchPublicRegistryOnArt() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val manager = WorkManager.getInstance(context)
        val name = "jh-schedule-art-" + java.util.UUID.randomUUID()
        val request = PeriodicWorkRequest.Builder(WorkRegistrationArtWorker::class.java,
            30, TimeUnit.MINUTES, 5, TimeUnit.MINUTES)
            .setInitialDelay(10, TimeUnit.MINUTES).build()
        val executor = Executors.newSingleThreadExecutor()
        try {
            manager.enqueueUniquePeriodicWork(name, ExistingPeriodicWorkPolicy.KEEP, request).result.get(10, TimeUnit.SECONDS)
            val result = AtomicReference<JankHunterWorkRegistryResult>()
            val done = CountDownLatch(1)
            WorkManagerRegistrySource(manager).request(executor) { result.set(it); done.countDown() }.use {
                assertTrue(done.await(10, TimeUnit.SECONDS))
                val snapshot = result.get() as JankHunterWorkRegistryResult.Snapshot
                val job = snapshot.jobs.single { row -> row.uuid == request.id.toString() }
                assertEquals(1_800_000L, job.repeatIntervalMs)
                assertEquals(300_000L, job.flexIntervalMs)
                assertEquals(600_000L, job.initialDelayMs)
                assertNotNull(job.earliestEligibleUnixMs)
            }
        } finally {
            manager.cancelUniqueWork(name).result.get(10, TimeUnit.SECONDS)
            executor.shutdownNow()
        }
    }
}
