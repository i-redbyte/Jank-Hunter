package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.Jhlog
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeAndroidComponentTelemetryTest {
    @Test
    fun serviceFlagsUseCallbackResultInsteadOfProcessImportanceGuessing() {
        assertEquals(
            Jhlog.COMPONENT_FLAG_STICKY,
            serviceComponentFlags(Jhlog.COMPONENT_SERVICE_START_COMMAND.toInt(), 1, null),
        )
        assertEquals(
            0L,
            serviceComponentFlags(Jhlog.COMPONENT_SERVICE_START_COMMAND.toInt(), 2, null),
        )
        assertEquals(
            Jhlog.COMPONENT_FLAG_BOUND,
            serviceComponentFlags(Jhlog.COMPONENT_SERVICE_BIND.toInt(), Int.MIN_VALUE, Any()),
        )
        assertEquals(
            0L,
            serviceComponentFlags(Jhlog.COMPONENT_SERVICE_BIND.toInt(), Int.MIN_VALUE, null),
        )
    }

    @Test
    fun packedTokensArePositiveUniqueAndRetainElapsedTimeWithoutRegistry() {
        val tokens = RuntimeTraceTokenSource(RuntimeLongSource { 42L })

        val first = tokens.start()
        val second = tokens.start()

        assertTrue(first > 0L)
        assertTrue(second > 0L)
        assertNotEquals(first, second)
        assertEquals(8L, RuntimeTraceTokenSource.durationUs(first, 50L))
    }

    @Test
    fun serviceTimeoutIsDistinctFromCallbackFailure() {
        assertEquals(
            Jhlog.COMPONENT_OUTCOME_TIMEOUT,
            serviceComponentOutcome(Jhlog.COMPONENT_SERVICE_TIMEOUT.toInt(), failed = false),
        )
        assertEquals(
            Jhlog.COMPONENT_OUTCOME_FAILURE,
            serviceComponentOutcome(Jhlog.COMPONENT_SERVICE_TIMEOUT.toInt(), failed = true),
        )
    }
}
