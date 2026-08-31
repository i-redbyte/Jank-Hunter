package io.jankhunter.runtime

import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class RuntimeHookGuardTest {
    private class FatalTestError : VirtualMachineError()

    @Test
    fun everySuppressedFailureIsCounted() {
        val before = RuntimeHookFailureTracker.total()
        val reasonBefore = RuntimeHookFailureTracker.count(RuntimeHookFailureReason.COLLECTOR)

        RuntimeHookGuard.run(RuntimeHookFailureReason.COLLECTOR) { error("run") }
        assertEquals(
            "fallback",
            RuntimeHookGuard.value("fallback", RuntimeHookFailureReason.COLLECTOR) { error("value") },
        )
        RuntimeHookGuard.swallow(RuntimeHookFailureReason.COLLECTOR) { error("swallow") }

        assertEquals(before + 3L, RuntimeHookFailureTracker.total())
        assertEquals(
            reasonBefore + 3L,
            RuntimeHookFailureTracker.count(RuntimeHookFailureReason.COLLECTOR),
        )
    }

    @Test
    fun fatalVmErrorsAreNeverSuppressed() {
        assertThrows(FatalTestError::class.java) { RuntimeHookGuard.run { throw FatalTestError() } }
        assertThrows(FatalTestError::class.java) {
            RuntimeHookGuard.value("fallback") { throw FatalTestError() }
        }
        assertThrows(FatalTestError::class.java) { RuntimeHookGuard.swallow { throw FatalTestError() } }
    }
}
