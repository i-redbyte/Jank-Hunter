package io.jankhunter.runtime.internal.system

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class MainThreadDispatchTrackerTest {
    @Test
    fun emitsDurationAndSourceAfterDispatchEnd() {
        var now = 1_000L
        val tracker = MainThreadDispatchTracker { now }

        assertNull(
            tracker.onMessage(
                ">>>>> Dispatching to Handler (android.view.Choreographer\$FrameHandler) {abc} callback: 0",
            ),
        )
        now = 1_042L

        assertEquals(
            MainThreadDispatchTracker.DispatchSample(
                durationMs = 42,
                source = "Handler (android.view.Choreographer\$FrameHandler)",
            ),
            tracker.onMessage("<<<<< Finished to Handler (android.view.Choreographer\$FrameHandler) {abc} callback"),
        )
    }

    @Test
    fun ignoresFinishWithoutStart() {
        val tracker = MainThreadDispatchTracker { 1_000L }

        assertNull(tracker.onMessage("<<<<< Finished to Handler (x) {abc} callback"))
    }
}
