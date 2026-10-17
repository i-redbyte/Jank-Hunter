package io.jankhunter.plugin.ui

import org.junit.Assert.*
import org.junit.Test

class BackgroundOwnershipTest {
    @Test fun ringWrapPreservesTheNewestTailAcrossChunks() {
        val buffer = CoalescingConsoleBuffer(6)
        assertTrue(buffer.offer("abcd"))
        assertFalse(buffer.offer("efgh"))
        assertFalse(buffer.offer("ij"))
        assertEquals("efghij", buffer.drain())
    }

    @Test fun disposedOwnerReleasesLateResourceWithoutDispatch() {
        val released = mutableListOf<String>()
        val pending = PendingResource<String>(released::add)
        pending.close()
        assertFalse(pending.publish("process"))
        assertEquals(listOf("process"), released)
        assertNull(pending.claim())
    }

    @Test fun disposeBeforeUiHandoffReleasesExactlyOnce() {
        val released = mutableListOf<String>()
        val pending = PendingResource<String>(released::add)
        assertTrue(pending.publish("process"))
        pending.close()
        pending.close()
        assertNull(pending.claim())
        assertEquals(listOf("process"), released)
    }

    @Test fun claimedResourceTransfersToTheUiOwner() {
        val released = mutableListOf<String>()
        val pending = PendingResource<String>(released::add)
        assertTrue(pending.publish("process"))
        assertEquals("process", pending.claim())
        pending.close()
        assertTrue(released.isEmpty())
    }

    @Test fun outputFloodQueuesOneBoundedUiDelivery() {
        val buffer = CoalescingConsoleBuffer(16)
        var scheduled = 0
        repeat(100_000) { if (buffer.offer("ab")) scheduled++ }
        assertEquals(1, scheduled)
        assertEquals("abababababababab", buffer.drain())
        assertTrue(buffer.offer("next"))
        assertEquals("next", buffer.drain())
    }

    @Test fun oversizedChunkAndDisposalNeverRetainUnboundedOutput() {
        val buffer = CoalescingConsoleBuffer(4)
        assertTrue(buffer.offer("0123456789"))
        assertEquals("6789", buffer.drain())
        assertTrue(buffer.offer("pending"))
        buffer.close()
        assertFalse(buffer.offer("late"))
        assertEquals("", buffer.drain())
    }
}
