package io.jankhunter.runtime.internal.system

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ProcThreadReaderTest {
    @Test fun limitsReadsAndMarksTruncation() {
        var reads = 0
        val reader = ProcThreadReader(
            enumerate = { listOf(1L, 2L, 3L).iterator() },
            readStat = { reads++; stat(it) },
            limit = 2,
        )
        val snapshot = reader.read()
        assertEquals(2, reads)
        assertEquals(2, snapshot.threads.size)
        assertFalse(snapshot.complete)
        assertEquals("thread_limit", snapshot.incompleteReason)
    }

    @Test fun disappearingFilesAndMismatchingIdentityNeverBecomeCompleteSnapshots() {
        val reader = ProcThreadReader(
            enumerate = { listOf(1L, 2L).iterator() },
            readStat = { if (it == 1L) stat(99) else null },
        )
        val snapshot = reader.read()
        assertTrue(snapshot.threads.isEmpty())
        assertFalse(snapshot.complete)
        assertEquals(2, snapshot.failedReads)
    }

    @Test fun enumerationFailureDiffersFromObservedEmptyProcess() {
        val failed = ProcThreadReader(enumerate = { null }, readStat = { null }).read()
        assertFalse(failed.complete)
        assertEquals("enumeration_unavailable", failed.incompleteReason)
        val empty = ProcThreadReader(enumerate = { emptyList<Long>().iterator() }, readStat = { null }).read()
        assertTrue(empty.complete)
    }

    private fun stat(tid: Long) = "$tid (worker) S 1 1 1 0 0 0 0 0 0 0 11 7 0 0 -5 2 1 0 42"
}
