package io.jankhunter.runtime

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class WorkerFailureCaptureTest {
    @Test fun capturesOnlyBoundedFramesWithoutMessage() {
        val failure = IllegalStateException("private-token")
        failure.stackTrace = Array(12) { StackTraceElement("app.SyncWorker", "run$it", "Source.kt", it) }
        val observed = captureWorkerFailure(73, 2, 1, 123, failure)

        assertEquals("java.lang.IllegalStateException", observed.exceptionClass)
        assertEquals(8, observed.stack.lines().size)
        assertTrue(observed.stackTruncated)
        assertTrue(observed.stack.contains("app.SyncWorker.run0:0"))
        assertFalse(observed.stack.contains("private-token"))
        assertFalse(observed.stack.contains("Source.kt"))
        assertTrue(observed.stack.toByteArray(Charsets.UTF_8).size <= 1024)
    }

    @Test fun longFrameNeverExceedsWireLimit() {
        val failure = IllegalArgumentException("secret")
        failure.stackTrace = arrayOf(StackTraceElement("a".repeat(1100), "run", "File.kt", 1))
        val observed = captureWorkerFailure(1, 0, 0, 1, failure)
        assertEquals("", observed.stack)
        assertTrue(observed.stackTruncated)
    }
}
