package io.jankhunter.workmanager

import com.google.common.util.concurrent.ListenableFuture
import org.junit.Assert.assertEquals
import org.junit.Test
import java.util.concurrent.CompletableFuture
import java.util.concurrent.Executor

class WorkerForegroundCaptureTest {
    @Test fun finishReleasesRecorderWhileAnObservedFutureIsStillPending() {
        val owner = Any()
        val capture = WorkerForegroundCapture { owner.hashCode() }
        val future = TestFuture()
        capture.observe(future)
        capture.finish()
        val field = WorkerForegroundCapture::class.java.getDeclaredField("record").apply { isAccessible = true }
        org.junit.Assert.assertNull("Pending listener must not retain the finished Worker's recorder", field.get(capture))
        org.junit.Assert.assertFalse(future.isDone)
    }

    @Test fun recordsOnlyConfirmedEntryAndAttemptBoundary() {
        val events = mutableListOf<Boolean>()
        val capture = WorkerForegroundCapture(events::add)
        val success = TestFuture()
        capture.observe(success)
        assertEquals(emptyList<Boolean>(), events)
        success.complete(null)
        capture.entered()
        capture.finish()
        capture.finish()
        assertEquals(listOf(true, false), events)
    }

    @Test fun failedOrLateTransitionsAreNotRecorded() {
        val events = mutableListOf<Boolean>()
        val capture = WorkerForegroundCapture(events::add)
        val failure = TestFuture()
        capture.observe(failure)
        failure.completeExceptionally(IllegalStateException("foreground denied"))
        val late = TestFuture()
        capture.observe(late)
        capture.finish()
        late.complete(null)
        assertEquals(emptyList<Boolean>(), events)
    }

    private class TestFuture : CompletableFuture<Void>(), ListenableFuture<Void> {
        override fun addListener(listener: Runnable, executor: Executor) {
            whenComplete { _, _ -> executor.execute(listener) }
        }
    }
}
