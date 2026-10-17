package io.jankhunter.runtime.internal.system

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertFalse
import org.junit.Test

class ThreadSnapshotTest {
    @Test fun parsesKernelIdentityWithoutConfusingNameOrJavaState() {
        val row = checkNotNull(parseThreadStat(stat(42, "pool ) worker", 900)))
        assertEquals(42L, row.tid)
        assertEquals("pool ) worker", row.name)
        assertEquals('S', row.osState)
        assertEquals(900L, row.startTicks)
        assertEquals(11L, row.userTicks)
        assertEquals(7L, row.systemTicks)
        assertEquals(-5L, row.priority)
        assertEquals(2L, row.nice)
    }

    @Test fun rejectsTruncatedNegativeAndOverflowCounters() {
        assertNull(parseThreadStat("42 (worker) S 1"))
        assertNull(parseThreadStat(stat(42, "worker", -1)))
        assertNull(parseThreadStat(stat(42, "worker", 1).replace("11 7", "9223372036854775808 7")))
        assertNull(parseThreadStat(stat(0, "worker", 1)))
    }

    @Test fun reusedTidHasDifferentKernelGeneration() {
        val old = checkNotNull(parseThreadStat(stat(42, "worker", 10)))
        val next = checkNotNull(parseThreadStat(stat(42, "worker", 20)))
        assertFalse(old.sameGeneration(next))
        assertEquals(true, old.sameGeneration(old.copy(name = "renamed")))
    }

    private fun stat(tid: Long, name: String, start: Long): String {
        return "$tid ($name) S 1 1 1 0 0 0 0 0 0 0 11 7 0 0 -5 2 1 0 $start"
    }
}
