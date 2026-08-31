package io.jankhunter.runtime

import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeProcessStateTest {
    @Test
    fun androidImportancePreservesForegroundServiceAsDistinctState() {
        assertEquals(RuntimeProcessImportance.FOREGROUND, RuntimeProcessImportance.fromAndroid(100))
        assertEquals(RuntimeProcessImportance.FOREGROUND_SERVICE, RuntimeProcessImportance.fromAndroid(125))
        assertEquals(RuntimeProcessImportance.PERCEPTIBLE, RuntimeProcessImportance.fromAndroid(130))
        assertEquals(RuntimeProcessImportance.VISIBLE, RuntimeProcessImportance.fromAndroid(200))
        assertEquals(RuntimeProcessImportance.SERVICE, RuntimeProcessImportance.fromAndroid(300))
        assertEquals(RuntimeProcessImportance.CACHED, RuntimeProcessImportance.fromAndroid(400))
        assertEquals(RuntimeProcessImportance.UNKNOWN, RuntimeProcessImportance.fromAndroid(-1))
    }

    @Test
    fun foregroundServiceKeepsAdaptiveSamplingActiveWithoutPretendingUiIsVisible() {
        val rawImportance = AtomicInteger(125)
        val state = RuntimeState()
        val access = access(state, rawImportance::get)

        assertFalse(access.isUiVisible())
        assertEquals(RuntimeUiVisibility.UNKNOWN, access.uiVisibility())
        assertEquals(RuntimeProcessImportance.FOREGROUND_SERVICE, access.processImportance())
        assertTrue(access.isUserRelevantForSampling())
    }

    @Test
    fun visibleUiKeepsSamplingActiveWhenProcessImportanceIsService() {
        val state = RuntimeState().apply {
            uiVisibility.set(RuntimeUiVisibility.VISIBLE.wireValue)
        }
        val access = access(state) { 300 }

        assertTrue(access.isUiVisible())
        assertEquals(RuntimeProcessImportance.SERVICE, access.processImportance())
        assertTrue(access.isUserRelevantForSampling())
    }

    @Test
    fun backgroundServiceIsNeitherVisibleUiNorUserRelevantSamplingState() {
        val state = RuntimeState().apply {
            uiVisibility.set(RuntimeUiVisibility.HIDDEN.wireValue)
        }
        val access = access(state) { 300 }

        assertFalse(access.isUiVisible())
        assertFalse(access.isUserRelevantForSampling())
    }

    private fun access(
        state: RuntimeState,
        processImportance: RuntimeIntSource,
    ): RuntimeTelemetryAccess {
        return RuntimeTelemetryAccess(
            state = state,
            contexts = ContextTracker(),
            coordinator = RuntimeCoordinator(state) { 1L },
            elapsedRealtimeMs = RuntimeLongSource { 1L },
            processImportance = processImportance,
        )
    }
}
