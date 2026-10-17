package io.jankhunter.sample.concurrency

import android.os.Handler
import android.os.HandlerThread
import io.jankhunter.runtime.JankHunterTrackedHandler
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger

/** Separate source sites let the CLI verify real hook admission across disable and restart. */
internal class ExecutionObservationProbe {
    private var calls = 0

    fun beforeDisable(): Int {
        val task = object : Runnable { override fun run() { calls++ } }
        task.run()
        return calls
    }

    fun whileDisabled(): Int {
        val task = object : Runnable { override fun run() { calls++ } }
        task.run()
        return calls
    }

    fun afterEnable(): Int {
        val task = object : Runnable { override fun run() { calls++ } }
        task.run()
        return calls
    }

    fun trackedRemoval(): Int {
        val thread = HandlerThread("observation-removal").apply { start() }
        val handler = Handler(thread.looper)
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val drained = CountDownLatch(1)
        val executions = AtomicInteger()
        try {
            check(handler.post { entered.countDown(); check(release.await(5L, TimeUnit.SECONDS)) })
            check(entered.await(2L, TimeUnit.SECONDS))
            val callback = Runnable { executions.incrementAndGet() }
            val tracked = JankHunterTrackedHandler(handler)
            val first = checkNotNull(tracked.post(callback))
            checkNotNull(tracked.postDelayed(callback, -1L))
            check(handler.post { drained.countDown() })
            check(first.requestRemoval())
            release.countDown()
            check(drained.await(2L, TimeUnit.SECONDS))
            return executions.get()
        } finally {
            release.countDown()
            thread.quitSafely()
            thread.join(3_000L)
            check(!thread.isAlive)
        }
    }
}
