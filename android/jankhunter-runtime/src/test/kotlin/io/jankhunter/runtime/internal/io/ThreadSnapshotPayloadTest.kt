package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.internal.system.ProcThreadSnapshot
import io.jankhunter.runtime.internal.system.ThreadObservation
import org.junit.Assert.assertArrayEquals
import org.junit.Test

class ThreadSnapshotPayloadTest {
    @Test fun versionThreePreservesSeparateJavaIdentityStateAndLossCounter() {
        val java = io.jankhunter.runtime.internal.system.JavaThreadObservation(7, "j", Thread.State.RUNNABLE, true, 3, "s")
        val row = ThreadObservation(42, "w", 'S', 9, 11, 7, -5, 2, java)
        val payload = BinaryPayload()
        encodeThreadSnapshot(payload, ProcThreadSnapshot(listOf(row), true, 0, null, true, 2), 100, 3, 5, 100)
        assertArrayEquals(byteArrayOf(
            3, 100, 3, 5, 100, 1, 0, 0, 1, 42, 9, 1, 119, 83, 11, 7, 9, 4,
            1, 7, 1, 3, 1, 106, 8, 82, 85, 78, 78, 65, 66, 76, 69, 1, 115, 0, 2,
        ), payload.copyBytes())
    }

    @Test fun versionThreeCarriesExplicitOwnershipWithoutChangingLegacyRows() {
        val java = io.jankhunter.runtime.internal.system.JavaThreadObservation(
            7, "maintenance", Thread.State.RUNNABLE, true, 3, "internal", ownedByJankHunter = true,
        )
        val payload = BinaryPayload()
        encodeThreadSnapshot(payload, ProcThreadSnapshot(listOf(ThreadObservation(42, "m", 'S', 9, 1, 1, 0, 0, java)), true, 0, null, true, 0), 100, 3, 5, 100)
        val bytes = payload.copyBytes()
        org.junit.Assert.assertEquals(3, bytes[0].toInt())
        org.junit.Assert.assertEquals(1, bytes[bytes.lastIndex - 1].toInt())
    }

    @Test fun preservesTickCountersAndSignedPriorityInVersionedPayload() {
        val snapshot = ProcThreadSnapshot(listOf(ThreadObservation(42, "w", 'S', 9, 11, 7, -5, 2)), true, 0, null)
        val payload = BinaryPayload()
        encodeThreadSnapshot(payload, snapshot, 100, 3, 5, 100)
        assertArrayEquals(byteArrayOf(1, 100, 3, 5, 100, 1, 0, 0, 1, 42, 9, 1, 119, 83, 11, 7, 9, 4), payload.copyBytes())
    }
    @Test fun JavaMetadataCannotOverflowRecordBudget() {
        val java = io.jankhunter.runtime.internal.system.JavaThreadObservation(7, "j".repeat(1024), Thread.State.RUNNABLE, true, 3, "s".repeat(1024))
        val rows = List(2048) { ThreadObservation(it + 1L, "w", 'S', 1, 1, 1, 0, 0, java) }
        val payload = BinaryPayload()
        encodeThreadSnapshot(payload, ProcThreadSnapshot(rows, true, 0, null, true, 0), 1, 1, 1000, 100)
        val bytes = payload.copyBytes()
        org.junit.Assert.assertTrue("oversized Java record: ${bytes.size}", bytes.size <= 240 * 1024)
        org.junit.Assert.assertEquals("complete flag", 0, bytes[6].toInt())
    }

    @Test fun longNamesCannotOverflowRecordAndLossIsExplicit() {
        val rows = List(2048) { ThreadObservation(it + 1L, "x".repeat(1024), 'S', 1, 1, 1, 0, 0) }
        val payload = BinaryPayload()
        encodeThreadSnapshot(payload, ProcThreadSnapshot(rows, true, 0, null), 1, 1, 1000, 100)
        val bytes = payload.copyBytes()
        org.junit.Assert.assertTrue("oversized record: ${bytes.size}", bytes.size <= 240 * 1024)
        org.junit.Assert.assertEquals("complete flag", 0, bytes[6].toInt())
        org.junit.Assert.assertTrue(String(bytes, Charsets.ISO_8859_1).contains("payload_limit"))
    }

}
