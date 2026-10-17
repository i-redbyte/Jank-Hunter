package io.jankhunter.workmanager

import android.content.Context
import android.os.Process
import androidx.lifecycle.Observer
import androidx.test.platform.app.InstrumentationRegistry
import androidx.work.OneTimeWorkRequest
import androidx.work.WorkInfo
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterConfig
import java.io.File
import java.util.UUID
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test

/** Run the two methods in separate am instrument invocations with am force-stop between them. */
class WorkProcessRestartArtTest {
    @Test fun stageOneEnqueuesDelayedWork() {
        assumeTrue(InstrumentationRegistry.getArguments().getString("jankhunterProcessRestartStage") == "one")
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val manager = WorkManager.getInstance(context)
        val request = OneTimeWorkRequest.Builder(RestartArtWorker::class.java)
            .setInitialDelay(15L, TimeUnit.SECONDS).build()
        File(context.filesDir, REQUEST_FILE).writeText("${request.id} ${Process.myPid()}\n")
        manager.enqueue(request).result.get(10L, TimeUnit.SECONDS)
        assertEquals(WorkInfo.State.ENQUEUED, manager.getWorkInfoById(request.id).get(10L, TimeUnit.SECONDS)?.state)
    }

    @Test fun stageTwoObservesExecutionInRestartedProcess() {
        assumeTrue(InstrumentationRegistry.getArguments().getString("jankhunterProcessRestartStage") == "two")
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val context = instrumentation.targetContext
        val (uuid, firstPid) = File(context.filesDir, REQUEST_FILE).readText().trim().split(' ')
        assertNotEquals(firstPid.toInt(), Process.myPid())
        val directory = File(context.filesDir, "work-process-restart")
        directory.deleteRecursively()
        val config = JankHunterConfig.builder().logDirectory(directory)
            .workerRegistrySource(WorkManagerRegistrySource(WorkManager.getInstance(context)))
            .workerRegistryIntervalMs(5_000L).runtimeCallGraphEnabled(false).metricAggregationEnabled(false).build()
        JankHunter.shutdown()
        instrumentation.runOnMainSync { JankHunter.init(context, config) }
        val manager = WorkManager.getInstance(context)
        val finished = CountDownLatch(1)
        val live = manager.getWorkInfoByIdLiveData(UUID.fromString(uuid))
        val observer = Observer<WorkInfo?> { if (it?.state == WorkInfo.State.SUCCEEDED) finished.countDown() }
        try {
            instrumentation.runOnMainSync { live.observeForever(observer) }
            assertTrue("persisted WorkManager request did not finish after process restart", finished.await(60L, TimeUnit.SECONDS))
            val workerPid = File(context.filesDir, WORKER_FILE).readText().trim().toInt()
            assertEquals(Process.myPid(), workerPid)
            assertNotEquals(firstPid.toInt(), workerPid)
            JankHunter.flush()
        } finally {
            instrumentation.runOnMainSync { live.removeObserver(observer) }
            instrumentation.runOnMainSync { JankHunter.shutdown() }
            manager.cancelWorkById(UUID.fromString(uuid)).result.get(10L, TimeUnit.SECONDS)
        }
        assertTrue(directory.walkTopDown().any { it.isFile && it.extension == "jhlog" })
    }

    private companion object {
        const val REQUEST_FILE = "jh-process-restart-request.txt"
        const val WORKER_FILE = "jh-process-restart-worker.txt"
    }
}

class RestartArtWorker(context: Context, parameters: WorkerParameters) : JankHunterWorker(context, parameters) {
    override fun doJankHunterWork(): Result {
        File(applicationContext.filesDir, "jh-process-restart-worker.txt").writeText("${Process.myPid()}\n")
        return Result.success()
    }
}
