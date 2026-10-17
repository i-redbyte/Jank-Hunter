package io.jankhunter.workmanager

import android.content.Context
import androidx.test.platform.app.InstrumentationRegistry
import androidx.work.OneTimeWorkRequest
import androidx.work.WorkManager
import androidx.work.Worker
import androidx.work.WorkerParameters
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterConfig
import io.jankhunter.runtime.JankHunterWorkRegistryResult
import io.jankhunter.runtime.JankHunterWorkRegistrySource
import io.jankhunter.runtime.JankHunterWorkState
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class WorkLateAttachArtTest {
    @Test fun runningWorkIsObservedAfterDiagnosticsAttachWithoutInventingItsStart() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val manager = WorkManager.getInstance(instrumentation.targetContext)
        val directory = File(instrumentation.context.filesDir, "work-late-attach")
        val request = OneTimeWorkRequest.Builder(LateAttachArtWorker::class.java).build()
        val runningObserved = CountDownLatch(1)
        val source = JankHunterWorkRegistrySource { executor, receive ->
            WorkManagerRegistrySource(manager).request(executor) { result ->
                if (result is JankHunterWorkRegistryResult.Snapshot && result.jobs.any {
                        it.uuid == request.id.toString() && it.state == JankHunterWorkState.RUNNING
                    }) runningObserved.countDown()
                receive(result)
            }
        }
        JankHunter.shutdown()
        directory.deleteRecursively()
        try {
            manager.enqueue(request).result.get(10L, TimeUnit.SECONDS)
            assertTrue("WorkManager did not start the worker", LateAttachArtWorker.started.await(10L, TimeUnit.SECONDS))
            val config = JankHunterConfig.builder().logDirectory(directory)
                .runtimeCallGraphEnabled(false).metricAggregationEnabled(false)
                .workerRegistrySource(source).workerRegistryIntervalMs(5_000L).build()
            instrumentation.runOnMainSync { JankHunter.init(instrumentation.targetContext, config) }
            assertTrue("late-attached registry did not observe RUNNING", runningObserved.await(15L, TimeUnit.SECONDS))
            JankHunter.flush()
        } finally {
            LateAttachArtWorker.release.countDown()
            manager.cancelWorkById(request.id).result.get(10L, TimeUnit.SECONDS)
            instrumentation.runOnMainSync { JankHunter.shutdown() }
        }
        assertEquals(1, directory.walkTopDown().count { it.isFile && it.extension == "jhlog" })
    }
}

class LateAttachArtWorker(context: Context, parameters: WorkerParameters) : Worker(context, parameters) {
    override fun doWork(): Result {
        started.countDown()
        return if (release.await(30L, TimeUnit.SECONDS)) Result.success() else Result.failure()
    }

    companion object {
        val started = CountDownLatch(1)
        val release = CountDownLatch(1)
    }
}
