package io.jankhunter.workmanager

import androidx.test.platform.app.InstrumentationRegistry
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.OneTimeWorkRequest
import androidx.work.PeriodicWorkRequest
import androidx.work.WorkManager
import androidx.work.Worker
import androidx.work.WorkerParameters
import android.content.Context
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterConfig
import java.io.File
import java.util.UUID
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test

class WorkRegistrationWireArtTest {
    @Test
    fun publicAdapterWritesObservedRegistrationsIncludingAnAlreadyExistingUUID() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val directory = File(instrumentation.context.filesDir, "work-registration-wire")
        JankHunter.shutdown()
        directory.deleteRecursively()
        val config = JankHunterConfig.builder().logDirectory(directory).autoStartCollectors(false)
            .runtimeCallGraphEnabled(false).metricAggregationEnabled(false).build()
        val manager = WorkManager.getInstance(instrumentation.targetContext)
        val unique = "jh-wire-" + UUID.randomUUID()
        val periodicName = unique + "-periodic"
        val batch = List(2) { request() }
        try {
            instrumentation.runOnMainSync { JankHunter.init(instrumentation.targetContext, config) }
            val first = request()
            manager.enqueueUniqueWorkWithJankHunter(unique, ExistingWorkPolicy.KEEP, first).result.get(5L, TimeUnit.SECONDS)
            drainQueries(manager)
            val ignored = request()
            manager.enqueueUniqueWorkWithJankHunter(unique, ExistingWorkPolicy.KEEP, ignored).result.get(5L, TimeUnit.SECONDS)
            assertNull(manager.getWorkInfoById(ignored.id).get(5L, TimeUnit.SECONDS))
            // The same UUID already exists: this observation must not claim a fresh insertion.
            manager.enqueueUniqueWorkWithJankHunter(unique, ExistingWorkPolicy.KEEP, first).result.get(5L, TimeUnit.SECONDS)
            drainQueries(manager)
            val replacement = request()
            manager.enqueueUniqueWorkWithJankHunter(unique, ExistingWorkPolicy.REPLACE, replacement).result.get(5L, TimeUnit.SECONDS)
            val appended = request()
            manager.enqueueUniqueWorkWithJankHunter(unique, ExistingWorkPolicy.APPEND, listOf(appended)).result.get(5L, TimeUnit.SECONDS)
            manager.enqueueWithJankHunter(batch).result.get(5L, TimeUnit.SECONDS)
            val periodic = periodicRequest()
            manager.enqueueUniquePeriodicWorkWithJankHunter(periodicName, ExistingPeriodicWorkPolicy.KEEP, periodic)
                .result.get(5L, TimeUnit.SECONDS)
            val ignoredPeriodic = periodicRequest()
            manager.enqueueUniquePeriodicWorkWithJankHunter(periodicName, ExistingPeriodicWorkPolicy.KEEP, ignoredPeriodic)
                .result.get(5L, TimeUnit.SECONDS)
            assertNull(manager.getWorkInfoById(ignoredPeriodic.id).get(5L, TimeUnit.SECONDS))
            assertNotNull(manager.getWorkInfoById(periodic.id).get(5L, TimeUnit.SECONDS))
            drainQueries(manager, 2)
            JankHunter.flush()
        } finally {
            manager.cancelUniqueWork(unique).result.get(5L, TimeUnit.SECONDS)
            manager.cancelUniqueWork(periodicName).result.get(5L, TimeUnit.SECONDS)
            batch.forEach { manager.cancelWorkById(it.id).result.get(5L, TimeUnit.SECONDS) }
            instrumentation.runOnMainSync { JankHunter.shutdown() }
        }
        val files = directory.walkTopDown().filter { it.isFile && it.extension == "jhlog" }.toList()
        assertEquals(1, files.size)
        files.single().copyTo(File(instrumentation.context.filesDir, "worker-registration-5.1.0.jhlog"), overwrite = true)
    }

    private fun request() = OneTimeWorkRequest.Builder(WorkRegistrationArtWorker::class.java)
        .setInitialDelay(1L, TimeUnit.DAYS).build()

    private fun periodicRequest() = PeriodicWorkRequest.Builder(
        WorkRegistrationArtWorker::class.java, 1L, TimeUnit.DAYS,
    ).setInitialDelay(1L, TimeUnit.DAYS).build()
}

/** WorkManager 2.11.2 completes Operation and UUID query callbacks on its serial task executor.
 * Consecutive public queries fence those callbacks, including the next query in a batch.
 * This is a bounded test barrier, not production polling or a claim about enqueue timestamps.
 */
internal fun drainQueries(manager: WorkManager, maximumBatchSize: Int = 1) {
    repeat(maximumBatchSize + 1) { manager.getWorkInfoById(UUID(0L, 0L)).get(10L, TimeUnit.SECONDS) }
}

class WorkRegistrationArtWorker(context: Context, parameters: WorkerParameters) : Worker(context, parameters) {
    override fun doWork(): Result = Result.success()
}
