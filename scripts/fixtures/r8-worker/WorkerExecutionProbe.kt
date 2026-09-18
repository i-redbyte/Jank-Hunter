package com.example.jhsmoke

import android.content.Context
import android.util.Log
import androidx.work.Data
import androidx.work.OneTimeWorkRequest
import androidx.work.WorkInfo
import androidx.work.WorkManager
import androidx.work.Worker
import androidx.work.WorkerParameters
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterWorkerRuntime
import io.jankhunter.workmanager.JankHunterCoroutineWorker
import io.jankhunter.workmanager.JankHunterWorker
import java.io.File
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.delay

/** Drives actual framework scheduling; all waits are confined to the fixture driver thread. */
object WorkerExecutionProbe {
    private val started = ConcurrentHashMap<String, CountDownLatch>()
    private val finished = ConcurrentHashMap<String, CountDownLatch>()

    @JvmStatic
    fun run(context: Context) {
        Thread({
            try {
                check(JankHunterWorkerRuntime.isActive()) { "Worker telemetry is inactive" }
                val manager = WorkManager.getInstance(context)
                for (worker in listOf(AsmWorker::class.java, AdapterWorker::class.java, SuspendedWorker::class.java)) {
                    for (mode in listOf("success", "failure", "retry")) runCase(manager, worker, mode)
                }
                runCase(manager, SuspendedWorker::class.java, "cancel")
                runCase(manager, SuspendedWorker::class.java, "exception")
                val archive = File(context.getExternalFilesDir(null), "worker-execution-${android.os.Process.myPid()}.zip")
                check(JankHunter.captureLogArchiveAsync(archive) { result ->
                    if (result == null) Log.e("JHWORKERR8", "EXECUTION FAIL archive capture returned null")
                    else Log.i("JHWORKERR8", "EXECUTION PASS cases=11 archive=$archive")
                })
            } catch (failure: Throwable) {
                Log.e("JHWORKERR8", "EXECUTION FAIL", failure)
            }
        }, "jh-worker-probe").start()
    }

    private fun runCase(manager: WorkManager, worker: Class<out androidx.work.ListenableWorker>, mode: String) {
        val request = OneTimeWorkRequest.Builder(worker)
            .setInputData(Data.Builder().putString("mode", mode).build()).build()
        val key = request.id.toString()
        val entered = CountDownLatch(1)
        val exited = CountDownLatch(1)
        started[key] = entered
        finished[key] = exited
        try {
            manager.enqueue(request).result.get(10, TimeUnit.SECONDS)
            check(entered.await(10, TimeUnit.SECONDS)) { "Worker did not start: $mode" }
            if (mode == "cancel") manager.cancelWorkById(request.id).result.get(10, TimeUnit.SECONDS)
            check(exited.await(10, TimeUnit.SECONDS)) { "Worker did not exit: $mode" }
            val expected = when (mode) {
                "success" -> WorkInfo.State.SUCCEEDED
                "retry" -> WorkInfo.State.ENQUEUED
                "cancel" -> WorkInfo.State.CANCELLED
                else -> WorkInfo.State.FAILED
            }
            val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(10)
            while (true) {
                val info = checkNotNull(manager.getWorkInfoById(request.id).get(10, TimeUnit.SECONDS))
                if (info.state == expected && (mode != "retry" || info.runAttemptCount > 0)) break
                check(System.nanoTime() < deadline) { "Wrong state $mode: ${info.state}" }
                Thread.sleep(20)
            }
            Log.i("JHWORKERR8", "EXECUTION worker=${worker.name} mode=$mode state=$expected")
        } finally {
            manager.cancelWorkById(request.id).result.get(10, TimeUnit.SECONDS)
            started.remove(key)
            finished.remove(key)
        }
    }

    private fun result(mode: String?): androidx.work.ListenableWorker.Result = when (mode) {
        "success" -> androidx.work.ListenableWorker.Result.success()
        "retry" -> androidx.work.ListenableWorker.Result.retry()
        else -> androidx.work.ListenableWorker.Result.failure()
    }

    class AsmWorker(context: Context, parameters: WorkerParameters) : Worker(context, parameters) {
        override fun doWork(): Result {
            started[id.toString()]?.countDown()
            return try { result(inputData.getString("mode")) } finally { finished[id.toString()]?.countDown() }
        }
    }

    class AdapterWorker(context: Context, parameters: WorkerParameters) : JankHunterWorker(context, parameters) {
        override fun doJankHunterWork(): Result {
            started[id.toString()]?.countDown()
            return try { result(inputData.getString("mode")) } finally { finished[id.toString()]?.countDown() }
        }
    }

    class SuspendedWorker(context: Context, parameters: WorkerParameters) : JankHunterCoroutineWorker(context, parameters) {
        override suspend fun doJankHunterWork(): Result {
            started[id.toString()]?.countDown()
            try {
                delay(30)
                return when (inputData.getString("mode")) {
                    "cancel" -> awaitCancellation()
                    "exception" -> error("Expected worker exception")
                    else -> result(inputData.getString("mode"))
                }
            } finally {
                finished[id.toString()]?.countDown()
            }
        }
    }
}
