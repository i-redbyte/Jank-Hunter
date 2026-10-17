package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterLogSnapshot
import java.io.File
import java.nio.file.Files
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class SnapshotFileCacheTest {
    @Test
    fun abandonedCopyIsRemovedBeforePublishingNextSnapshot() {
        val root = Files.createTempDirectory("jh-snapshot-recovery").toFile()
        try {
            val cache = File(root, "cache").apply { mkdirs() }
            val abandoned = File(cache, ".00112233445566778899aabbccddeeff.tmp").apply { mkdirs() }
            File(abandoned, "partial.jhlog").writeBytes(ByteArray(2048))
            val live = File(root, "live.jhlog").apply { writeBytes(byteArrayOf(1)) }
            SnapshotFileCache.materialize(JankHunterLogSnapshot(1L, listOf(live.path), logByteLimits = listOf(1L)), cache, 1024L).use {
                assertFalse(abandoned.exists())
            }
            assertTrue(live.exists())
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun copyPreservesFrontierAfterFurtherWrites() {
        val root = Files.createTempDirectory("jh-snapshot-cache-test").toFile()
        try {
            val live = File(root, "live.jhlog").apply { writeBytes(byteArrayOf(1, 2, 3)) }
            val snapshot = JankHunterLogSnapshot(1L, listOf(live.path), logByteLimits = listOf(2L))
            val copied = SnapshotFileCache.materialize(snapshot, File(root, "cache"), 1024L)
            val exported = File(copied.logPaths.single())
            assertNotEquals(live.path, exported.path)
            live.appendBytes(byteArrayOf(4, 5))
            assertArrayEquals(byteArrayOf(1, 2), exported.readBytes())
            assertTrue(live.exists())
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun cacheEvictsOldCopiesUnderItsBudgetWithoutDeletingLiveRecording() {
        val root = Files.createTempDirectory("jh-snapshot-cache-budget").toFile()
        try {
            val live = File(root, "live.jhlog").apply { writeBytes(byteArrayOf(1, 2, 3)) }
            val cache = File(root, "cache")
            val snapshot = JankHunterLogSnapshot(1L, listOf(live.path), logByteLimits = listOf(3L))
            val first = SnapshotFileCache.materialize(snapshot, cache, 3L)
            val second = SnapshotFileCache.materialize(snapshot, cache, 3L)
            assertFalse(File(first.logPaths.single()).exists())
            assertArrayEquals(live.readBytes(), File(second.logPaths.single()).readBytes())
            assertTrue(live.exists())
        } finally {
            root.deleteRecursively()
        }
    }
}
