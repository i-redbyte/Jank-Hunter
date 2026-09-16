package io.jankhunter.runtime.internal.system

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeMainThreadDispatcherTest {
    @Test
    fun backgroundCleanupIsPostedInsteadOfRunningAgainstMainCallbacks() {
        val posted = mutableListOf<() -> Unit>()
        var cleaned = false
        val dispatcher = RuntimeMainThreadDispatcher(
            isMainThread = { false },
            postToMain = { task ->
                posted += task
                true
            },
        )

        assertTrue(dispatcher.dispatch { cleaned = true })
        assertFalse(cleaned)
        assertEquals(1, posted.size)

        posted.single().invoke()
        assertTrue(cleaned)
    }

    @Test
    fun rejectedMainLooperAccessIsReportedToTheCaller() {
        val dispatcher = RuntimeMainThreadDispatcher(
            isMainThread = { false },
            postToMain = { error("main looper unavailable") },
        )

        assertFalse(dispatcher.dispatch {})
    }
}
