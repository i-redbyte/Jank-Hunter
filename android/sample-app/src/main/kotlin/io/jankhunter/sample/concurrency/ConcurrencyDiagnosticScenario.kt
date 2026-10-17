package io.jankhunter.sample.concurrency

import android.content.Context
import android.os.Handler
import android.os.Looper
import androidx.work.BackoffPolicy
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.WorkManager
import io.jankhunter.runtime.JankHunterResources
import io.jankhunter.runtime.JankHunterTrackedHandler
import java.io.File
import java.util.UUID
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.ThreadFactory
import java.util.concurrent.ThreadPoolExecutor
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.locks.ReentrantLock
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** Deterministic boundaries for inspecting causal links; this is intentionally inefficient app code. */
internal class ConcurrencyDiagnosticScenario(private val context: Context) {
    fun enqueueRetryWorker(): UUID {
        val work = OneTimeWorkRequestBuilder<RetryScenarioWorker>()
            .setBackoffCriteria(BackoffPolicy.LINEAR, 10, TimeUnit.SECONDS)
            .build()
        WorkManager.getInstance(context).enqueue(work).result.get(5, TimeUnit.SECONDS)
        return work.id
    }

    suspend fun run() {
        shortLivedThreads()
        queuedAndCancelledWork()
        repeatedScheduledWork()
        migratingCoroutine()
        originalHandlerIdentity()
        trackedHandlerDispatch()
    }

    private fun shortLivedThreads() {
        repeat(12) {
            val thread = Thread({ exerciseResources() }, "sample-short-lived")
            thread.start()
            thread.join(TimeUnit.SECONDS.toMillis(5))
            check(!thread.isAlive) { "scenario thread did not finish" }
        }
    }

    private fun queuedAndCancelledWork() {
        val started = CountDownLatch(1)
        val release = CountDownLatch(1)
        val pool = ThreadPoolExecutor(1, 1, 0, TimeUnit.MILLISECONDS, ArrayBlockingQueue(16),
            ThreadFactory { runnable -> Thread(runnable, "sample-causal-pool") })
        try {
            val first = pool.submit {
                started.countDown()
                check(release.await(5, TimeUnit.SECONDS))
            }
            check(started.await(5, TimeUnit.SECONDS))
            val cancelled = pool.submit { error("cancelled task must not run") }
            check(cancelled.cancel(false))
            val queued = pool.submit { exerciseResources() }
            // A controlled interval creates a known queue backlog without relying on random sleep.
            CountDownLatch(1).await(400, TimeUnit.MILLISECONDS)
            release.countDown()
            first.get(5, TimeUnit.SECONDS)
            queued.get(5, TimeUnit.SECONDS)
        } finally {
            release.countDown()
            pool.shutdownNow()
            check(pool.awaitTermination(5, TimeUnit.SECONDS))
        }
    }

    private fun repeatedScheduledWork() {
        val scheduler = Executors.newSingleThreadScheduledExecutor()
        val occurrences = CountDownLatch(3)
        try {
            val future = scheduler.scheduleAtFixedRate({ occurrences.countDown() }, 0, 20, TimeUnit.MILLISECONDS)
            check(occurrences.await(5, TimeUnit.SECONDS))
            future.cancel(false)
        } finally {
            scheduler.shutdownNow()
            check(scheduler.awaitTermination(5, TimeUnit.SECONDS))
        }
    }

    private suspend fun migratingCoroutine() = coroutineScope {
        launch(Dispatchers.Default) {
            exerciseResources()
            withContext(Dispatchers.IO) { exerciseResources() }
            delay(20)
            exerciseResources()
        }.join()
    }

    private fun originalHandlerIdentity() {
        val handler = Handler(Looper.getMainLooper())
        val ran = AtomicInteger()
        val callback = Runnable { ran.incrementAndGet() }
        handler.postDelayed(callback, 30_000)
        handler.postDelayed(callback, 30_000)
        handler.removeCallbacks(callback)
        check(ran.get() == 0)
    }

    private fun trackedHandlerDispatch() {
        val handler = JankHunterTrackedHandler(Handler(Looper.getMainLooper()))
        val delivered = CountDownLatch(1)
        val calls = AtomicInteger()
        val callback = Runnable {
            calls.incrementAndGet()
            exerciseResources()
            delivered.countDown()
        }
        val cancelled = checkNotNull(handler.postDelayed(callback, 30_000))
        check(cancelled.requestRemoval())
        checkNotNull(handler.post(callback))
        check(delivered.await(5, TimeUnit.SECONDS))
        check(calls.get() == 1)
    }

    private fun exerciseResources() {
        JankHunterResources.withLock(ReentrantLock()) {
            JankHunterResources.withMonitor(this) {
                val file = File(context.cacheDir, "concurrency-scenario.bin")
                val payload = ByteArray(4096) { it.toByte() }
                file.writeBytes(payload)
                check(file.readBytes().contentEquals(payload))
                JankHunterResources.use(AutoCloseable { }) { }
            }
        }
    }
}
