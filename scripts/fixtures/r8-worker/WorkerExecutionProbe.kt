package com.example.jhsmoke

import android.content.Context
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.pm.ServiceInfo
import android.os.Build
import android.util.Log
import androidx.work.Data
import androidx.work.ForegroundInfo
import androidx.work.OneTimeWorkRequest
import androidx.work.WorkInfo
import androidx.work.WorkManager
import androidx.work.Worker
import androidx.work.WorkerParameters
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterIOHooks
import io.jankhunter.runtime.JankHunterWorkerRuntime
import io.jankhunter.workmanager.JankHunterCoroutineWorker
import io.jankhunter.workmanager.enqueueWithJankHunter
import io.jankhunter.workmanager.enqueueUniqueWorkWithJankHunter
import io.jankhunter.workmanager.JankHunterWorker
import java.io.File
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.Executors
import kotlin.coroutines.resume
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.delay

/** Drives actual framework scheduling; all waits are confined to the fixture driver thread. */
object WorkerExecutionProbe {
    private val started = ConcurrentHashMap<String, CountDownLatch>()
    private val finished = ConcurrentHashMap<String, CountDownLatch>()

    @JvmStatic
    fun run(context: Context) {
        Thread({
            val releaseIdentityProbe = CountDownLatch(1)
            try {
                check(JankHunterWorkerRuntime.isActive()) { "Worker telemetry is inactive" }
                val manager = WorkManager.getInstance(context)
                val registryObserved = CountDownLatch(2)
                val registry = io.jankhunter.workmanager.WorkManagerRegistrySource(manager)
                check(JankHunter.reconfigure("registry-probe") { builder ->
                    builder.threadSamplerEnabled(true).threadSampleIntervalMs(5000)
                        .workerRegistryIntervalMs(5000).workerRegistrySource { executor, receive ->
                        registry.request(executor) { snapshot -> receive(snapshot); registryObserved.countDown() }
                    }
                })
                JankHunterIOHooks.writeFileBytes(File(context.cacheDir, "jh-resource-probe"),
                    byteArrayOf(1, 2, 3), 0x4A485245534F5552L, "com.example.jhsmoke.WorkerExecutionProbe.run")
                val migrationExecutor = Executors.newSingleThreadExecutor { task -> Thread(task, "jh-coroutine-resume") }
                try {
                    runBlocking {
                        val job = launch(Dispatchers.Unconfined) {
                            suspendCancellableCoroutine<Unit> { continuation ->
                                migrationExecutor.execute { continuation.resume(Unit) }
                            }
                        }
                        job.join()
                    }
                } finally {
                    migrationExecutor.shutdown()
                    check(migrationExecutor.awaitTermination(5, TimeUnit.SECONDS)) { "Coroutine executor did not stop" }
                }
                io.jankhunter.runtime.JankHunterTelemetry.traceOperation("thread-probe") {
                    val factory: java.util.concurrent.ThreadFactory = ShortThreadFactory()
                    val short = checkNotNull(factory.newThread(ShortThreadRun()))
                    short.start()
                    short.join(5000)
                    check(!short.isAlive) { "Short thread did not complete" }
                }
                val nullFactory: java.util.concurrent.ThreadFactory = java.util.concurrent.ThreadFactory { null }
                check(nullFactory.newThread(Runnable {}) == null) { "Factory result changed" }
                val failed = object : Thread("jh-failed-start") {
                    override fun start() { error("Expected start failure") }
                }
                check(runCatching { failed.start() }.exceptionOrNull()?.message == "Expected start failure")
                val identityEntered = CountDownLatch(1)
                Thread(ThreadIdentityProbe(identityEntered, releaseIdentityProbe), "jh-identity-r8-java").apply { isDaemon = true; start() }
                check(identityEntered.await(10, TimeUnit.SECONDS)) { "Thread identity probe did not start" }
                val delayed = OneTimeWorkRequest.Builder(RegistryWorker::class.java).setInitialDelay(1, TimeUnit.HOURS).build()
                val dependent = OneTimeWorkRequest.Builder(RegistryWorker::class.java).build()
                manager.beginUniqueWork("registry-probe", androidx.work.ExistingWorkPolicy.REPLACE, delayed)
                    .then(dependent).enqueue().result.get(10, TimeUnit.SECONDS)
                val left = OneTimeWorkRequest.Builder(RegistryWorker::class.java).setInitialDelay(1, TimeUnit.HOURS).build()
                val right = OneTimeWorkRequest.Builder(RegistryWorker::class.java).setInitialDelay(1, TimeUnit.HOURS).build()
                val joined = OneTimeWorkRequest.Builder(RegistryWorker::class.java).build()
                androidx.work.WorkContinuation.combine(listOf(manager.beginWith(left), manager.beginWith(right)))
                    .then(joined).enqueue().result.get(10, TimeUnit.SECONDS)
                val nestedEnqueues = arrayOf(
                    manager.enqueue(OneTimeWorkRequest.Builder(RegistryWorker::class.java).setInitialDelay(1, TimeUnit.HOURS).build()),
                    manager.enqueue(OneTimeWorkRequest.Builder(RegistryWorker::class.java).setInitialDelay(1, TimeUnit.HOURS).build()),
                )
                nestedEnqueues.forEach { it.result.get(10, TimeUnit.SECONDS) }
                io.jankhunter.runtime.JankHunterTelemetry.traceOperation("submission-probe") {
                    val first = OneTimeWorkRequest.Builder(RegistryWorker::class.java)
                        .setBackoffCriteria(androidx.work.BackoffPolicy.LINEAR, 15, TimeUnit.SECONDS)
                        .setInitialDelay(1, TimeUnit.HOURS).build()
                    val ignored = OneTimeWorkRequest.Builder(RegistryWorker::class.java)
                        .setExpedited(androidx.work.OutOfQuotaPolicy.DROP_WORK_REQUEST).build()
                    manager.enqueueUniqueWork("r8-unique-intent", androidx.work.ExistingWorkPolicy.KEEP, first).result.get(10, TimeUnit.SECONDS)
                    manager.enqueueUniqueWorkWithJankHunter("r8-unique-intent", androidx.work.ExistingWorkPolicy.KEEP, ignored).result.get(10, TimeUnit.SECONDS)
                    check(manager.getWorkInfoById(ignored.id).get(10, TimeUnit.SECONDS) == null) { "KEEP unexpectedly accepted second UUID" }
                    val periodic = androidx.work.PeriodicWorkRequest.Builder(RegistryWorker::class.java, 15, TimeUnit.MINUTES)
                        .setInitialDelay(1, TimeUnit.HOURS).build()
                    manager.enqueueUniquePeriodicWork("r8-periodic-intent", androidx.work.ExistingPeriodicWorkPolicy.UPDATE, periodic).result.get(10, TimeUnit.SECONDS)
                }
                val directBatch = List(2) {
                    OneTimeWorkRequest.Builder(RegistryWorker::class.java).setInitialDelay(1, TimeUnit.HOURS).build()
                }
                manager.enqueue(directBatch).result.get(10, TimeUnit.SECONDS)
                check(runCatching { manager.enqueue(emptyList<androidx.work.WorkRequest>()) }.exceptionOrNull() is IllegalArgumentException) {
                    "Empty enqueue did not preserve original exception"
                }
                for (worker in listOf(AsmWorker::class.java, AdapterWorker::class.java, SuspendedWorker::class.java)) {
                    for (mode in listOf("success", "failure", "retry", "exception")) runCase(manager, worker, mode)
                }
                runCase(manager, SuspendedWorker::class.java, "cancel")
                check(registryObserved.await(15, TimeUnit.SECONDS)) { "Registry observations did not arrive" }
                val archive = File(context.getExternalFilesDir(null), "worker-execution-${android.os.Process.myPid()}.zip")
                check(JankHunter.captureLogArchiveAsync(archive) { result ->
                    releaseIdentityProbe.countDown()
                    if (result == null) Log.e("JHWORKERR8", "EXECUTION FAIL archive capture returned null")
                    else Log.i("JHWORKERR8", "EXECUTION PASS cases=13 archive=$archive")
                })
            } catch (failure: Throwable) {
                releaseIdentityProbe.countDown()
                Log.e("JHWORKERR8", "EXECUTION FAIL", failure)
            }
        }, "jh-worker-probe").start()
    }

    private class ShortThreadFactory : java.util.concurrent.ThreadFactory {
        override fun newThread(task: Runnable): Thread = Thread(task, "jh-short-lived-probe")
    }
    private class ShortThreadRun : Runnable { override fun run() = Unit }

    private class ThreadIdentityProbe(private val entered: CountDownLatch, private val release: CountDownLatch) : Runnable {
        override fun run() {
            entered.countDown()
            check(release.await(30, TimeUnit.SECONDS)) { "Thread identity probe was not released" }
        }
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
            val operation = if (worker == AsmWorker::class.java) manager.enqueue(request) else manager.enqueueWithJankHunter(request)
            operation.result.get(10, TimeUnit.SECONDS)
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

    private fun foregroundInfo(context: Context): ForegroundInfo {
        val manager = context.getSystemService(NotificationManager::class.java)
        if (Build.VERSION.SDK_INT >= 26) {
            manager.createNotificationChannel(NotificationChannel("jh-smoke", "Jank Hunter smoke", NotificationManager.IMPORTANCE_LOW))
        }
        val notification = if (Build.VERSION.SDK_INT >= 26) Notification.Builder(context, "jh-smoke")
            else Notification.Builder(context)
        notification.setSmallIcon(android.R.drawable.stat_notify_sync).setContentTitle("Worker smoke")
        val type = if (Build.VERSION.SDK_INT >= 29) ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC else 0
        return ForegroundInfo(77, notification.build(), type)
    }

    class RegistryWorker(context: Context, parameters: WorkerParameters) : Worker(context, parameters) {
        override fun doWork(): Result = Result.success()
    }

    // Deliberately never referenced at runtime: inventory must not depend on execution.
    class NeverRunWorker(context: Context, parameters: WorkerParameters) : Worker(context, parameters) {
        override fun doWork(): Result = Result.success()
    }

    class AsmWorker(context: Context, parameters: WorkerParameters) : Worker(context, parameters) {
        override fun doWork(): Result {
            started[id.toString()]?.countDown()
            return try {
                if (inputData.getString("mode") == "exception") error("Expected worker exception")
                result(inputData.getString("mode"))
            } finally { finished[id.toString()]?.countDown() }
        }
    }

    class AdapterWorker(context: Context, parameters: WorkerParameters) : JankHunterWorker(context, parameters) {
        override fun doJankHunterWork(): Result {
            started[id.toString()]?.countDown()
            return try {
                if (inputData.getString("mode") == "success") {
                    setJankHunterForegroundAsync(foregroundInfo(applicationContext)).get(10, TimeUnit.SECONDS)
                }
                setProgressAsync(Data.Builder().putInt("step", 1).build())
                if (inputData.getString("mode") == "exception") error("Expected worker exception")
                result(inputData.getString("mode"))
            } finally { finished[id.toString()]?.countDown() }
        }
    }

    class SuspendedWorker(context: Context, parameters: WorkerParameters) : JankHunterCoroutineWorker(context, parameters) {
        override suspend fun doJankHunterWork(): Result {
            started[id.toString()]?.countDown()
            try {
                if (inputData.getString("mode") == "success") {
                    setJankHunterForeground(foregroundInfo(applicationContext))
                }
                setProgress(Data.Builder().putInt("step", 1).build())
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
