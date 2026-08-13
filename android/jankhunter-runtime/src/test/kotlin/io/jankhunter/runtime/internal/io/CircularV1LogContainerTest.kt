package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterRandomAccessBinaryWriter
import java.io.File
import java.io.IOException
import java.io.RandomAccessFile
import java.nio.file.Files
import java.util.Random
import java.util.zip.CRC32
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class CircularV1LogContainerTest {
    @Test
    fun deterministicStateMachinePreservesWindowAndExactAccounting() {
        val directory = Files.createTempDirectory("jankhunter-v1-state-machine").toFile()
        val file = File(directory, "state-machine.jhlog")
        val limit = JhlogV1.MIN_FILE_BYTES
        val random = Random(32_153L)
        val chunkCrcs = ArrayList<Long>()
        var generatedChunkBytes = 0L
        var expectedWraps = 0L
        var previousFirstSequence = 0L
        try {
            CircularV1LogContainer(file, limit).use { container ->
                container.writeFileHeader(ByteArray(73) { index -> (index * 17).toByte() })
                val history = ByteArray(JhlogV1.HISTORY_MAX_BYTES) { index -> (index * 31).toByte() }
                val liveA = livePayload(generation = 2L, fill = 0x35)
                val liveB = livePayload(generation = 3L, fill = 0x57)
                assertTrue(container.writeGrowthHistory(history))
                assertTrue(container.writeGrowthLive(liveA))
                assertTrue(container.writeGrowthLive(liveB))

                repeat(160) { sequence ->
                    val storedSize = when (sequence % 11) {
                        0 -> 1
                        1 -> 4_096
                        2 -> 220_000
                        else -> 1 + random.nextInt(128_000)
                    }
                    val payload = deterministicPayload(sequence, storedSize)
                    val crc = crc32(payload)
                    val committed = container.commitChunk(
                        flags = 0,
                        sequence = sequence.toLong(),
                        stored = payload,
                        rawSize = payload.size,
                        recordCount = 1,
                        rawCrc = crc,
                        terminalReserveBytes = 0L,
                    )
                    val total = payload.size.toLong() + JhlogV1.CHUNK_HEADER_BYTES + JhlogV1.COMMIT_TRAILER_BYTES
                    assertEquals(total, committed)
                    generatedChunkBytes += total
                    chunkCrcs += crc

                    val state = readLatestState(file)
                    if (state.firstSequence > previousFirstSequence) expectedWraps++
                    previousFirstSequence = state.firstSequence
                    assertStateInvariants(
                        file = file,
                        state = state,
                        limit = limit,
                        generatedChunkBytes = generatedChunkBytes,
                        expectedNextSequence = sequence.toLong() + 1L,
                        expectedWraps = expectedWraps,
                    )
                    if (sequence % 13 == 0 || sequence == 159) {
                        val chunks = readLogicalChunks(file, state)
                        assertEquals(state.chunkCount.toInt(), chunks.size)
                        chunks.forEach { chunk ->
                            assertEquals(chunkCrcs[chunk.sequence.toInt()], chunk.rawCrc)
                            assertEquals(chunk.rawCrc, crc32(chunk.payload))
                        }
                    }
                }

                assertArrayEquals(
                    history,
                    readBytes(file, JhlogV1.HISTORY_OFFSET, history.size),
                )
                assertArrayEquals(
                    liveA,
                    readBytes(file, JhlogV1.LIVE_SUMMARY_A_OFFSET, liveA.size),
                )
                assertArrayEquals(
                    liveB,
                    readBytes(file, JhlogV1.LIVE_SUMMARY_B_OFFSET, liveB.size),
                )
            }
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun exactArenaChunkIsReplacedByOneSmallCommittedChunk() {
        val directory = Files.createTempDirectory("jankhunter-v1-arena-boundary").toFile()
        val file = File(directory, "arena-boundary.jhlog")
        val limit = JhlogV1.MIN_FILE_BYTES
        val arenaCapacity = limit - JhlogV1.ARENA_OFFSET
        val exactPayloadBytes = (
            arenaCapacity - JhlogV1.CHUNK_HEADER_BYTES - JhlogV1.COMMIT_TRAILER_BYTES
            ).toInt()
        try {
            CircularV1LogContainer(file, limit).use { container ->
                container.writeFileHeader(byteArrayOf(1, 2, 3))
                val exact = deterministicPayload(0, exactPayloadBytes)
                container.commitChunk(0, 0L, exact, exact.size, 1, crc32(exact), 0L)

                val full = readLatestState(file)
                assertEquals(arenaCapacity, full.usedBytes)
                assertEquals(arenaCapacity, full.tail)
                assertEquals(limit, file.length())
                assertEquals(listOf(0L), readLogicalChunks(file, full).map(LogicalChunk::sequence))

                val replacement = byteArrayOf(11, 22, 33)
                container.commitChunk(0, 1L, replacement, replacement.size, 1, crc32(replacement), 0L)

                val replaced = readLatestState(file)
                assertEquals(1L, replaced.firstSequence)
                assertEquals(2L, replaced.nextSequence)
                assertEquals(1L, replaced.chunkCount)
                assertEquals(1L, replaced.wraps)
                assertEquals(1L, replaced.evictedChunks)
                assertEquals(arenaCapacity, replaced.evictedBytes)
                assertEquals(file.length(), replaced.retainedBytes)
                assertEquals(limit, file.length())
                val chunks = readLogicalChunks(file, replaced)
                assertEquals(listOf(1L), chunks.map(LogicalChunk::sequence))
                assertArrayEquals(replacement, chunks.single().payload)
            }
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun rejectedWritesLeavePublishedStateUntouched() {
        val directory = Files.createTempDirectory("jankhunter-v1-rejected-write").toFile()
        val file = File(directory, "rejected-write.jhlog")
        val limit = JhlogV1.MIN_FILE_BYTES
        try {
            val container = CircularV1LogContainer(file, limit)
            container.writeFileHeader(byteArrayOf(1))
            val first = byteArrayOf(1, 2, 3, 4)
            container.commitChunk(0, 0L, first, first.size, 1, crc32(first), 0L)
            val before = readLatestState(file)
            val beforeBytes = file.readBytes()

            assertThrows(IOException::class.java) {
                container.commitChunk(0, 2L, first, first.size, 1, crc32(first), 0L)
            }
            val oversized = ByteArray((limit - JhlogV1.ARENA_OFFSET).toInt())
            assertThrows(IOException::class.java) {
                container.commitChunk(0, 1L, oversized, oversized.size, 1, crc32(oversized), 0L)
            }
            assertEquals(before, readLatestState(file))
            assertArrayEquals(beforeBytes, file.readBytes())

            container.close()
            assertThrows(IOException::class.java) {
                container.commitChunk(0, 1L, first, first.size, 1, crc32(first), 0L)
            }
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun interruptedAppendNeverPublishesPartialChunk() {
        val directory = Files.createTempDirectory("jankhunter-v1-interrupted-append").toFile()
        try {
            for (failedWrite in 1..4) {
                val file = File(directory, "append-$failedWrite.jhlog")
                val writer = FaultInjectingWriter(file)
                val container = CircularV1LogContainer(writer, JhlogV1.MIN_FILE_BYTES)
                container.writeFileHeader(byteArrayOf(1, 2, 3))
                val first = deterministicPayload(0, 4_096)
                container.commitChunk(0, 0L, first, first.size, 1, crc32(first), 0L)
                val before = readLatestState(file)

                writer.failPartiallyOnWrite(failedWrite)
                val second = deterministicPayload(1, 8_192)
                assertThrows(IOException::class.java) {
                    container.commitChunk(0, 1L, second, second.size, 1, crc32(second), 0L)
                }
                container.close()

                val recovered = readLatestState(file)
                assertEquals("failure at write $failedWrite", before, recovered)
                val chunks = readLogicalChunks(file, recovered)
                assertEquals(listOf(0L), chunks.map(LogicalChunk::sequence))
                assertArrayEquals(first, chunks.single().payload)
            }
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun interruptedWrapPublishesEitherOldWindowOrSafeEmptyWindow() {
        val directory = Files.createTempDirectory("jankhunter-v1-interrupted-wrap").toFile()
        val limit = JhlogV1.MIN_FILE_BYTES
        val arenaCapacity = limit - JhlogV1.ARENA_OFFSET
        val exactPayloadBytes = (
            arenaCapacity - JhlogV1.CHUNK_HEADER_BYTES - JhlogV1.COMMIT_TRAILER_BYTES
            ).toInt()
        try {
            for (failedWrite in 1..5) {
                val file = File(directory, "wrap-$failedWrite.jhlog")
                val writer = FaultInjectingWriter(file)
                val container = CircularV1LogContainer(writer, limit)
                container.writeFileHeader(byteArrayOf(7))
                val first = deterministicPayload(0, exactPayloadBytes)
                container.commitChunk(0, 0L, first, first.size, 1, crc32(first), 0L)

                writer.failPartiallyOnWrite(failedWrite)
                val replacement = deterministicPayload(1, 128)
                assertThrows(IOException::class.java) {
                    container.commitChunk(0, 1L, replacement, replacement.size, 1, crc32(replacement), 0L)
                }
                container.close()

                val recovered = readLatestState(file)
                if (failedWrite == 1) {
                    assertEquals(1L, recovered.chunkCount)
                    assertEquals(listOf(0L), readLogicalChunks(file, recovered).map(LogicalChunk::sequence))
                } else {
                    assertEquals("failure at write $failedWrite", 0L, recovered.chunkCount)
                    assertEquals(1L, recovered.firstSequence)
                    assertEquals(1L, recovered.nextSequence)
                    assertTrue(readLogicalChunks(file, recovered).isEmpty())
                }
            }
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun constructorRejectsUnsafeLimitsAndNonEmptyTargets() {
        val directory = Files.createTempDirectory("jankhunter-v1-open-contract").toFile()
        try {
            val tooSmall = File(directory, "too-small.jhlog")
            assertThrows(IOException::class.java) {
                CircularV1LogContainer(tooSmall, JhlogV1.MIN_FILE_BYTES - 1L)
            }
            val unlimited = File(directory, "unlimited.jhlog")
            assertThrows(IOException::class.java) {
                CircularV1LogContainer(unlimited, Long.MAX_VALUE)
            }
            val nonEmpty = File(directory, "non-empty.jhlog").apply { writeBytes(byteArrayOf(1)) }
            assertThrows(IOException::class.java) {
                CircularV1LogContainer(nonEmpty, JhlogV1.MIN_FILE_BYTES)
            }
            assertFalse(tooSmall.exists() && tooSmall.length() > 0L)
            assertArrayEquals(byteArrayOf(1), nonEmpty.readBytes())
        } finally {
            directory.deleteRecursively()
        }
    }

    private fun assertStateInvariants(
        file: File,
        state: V1State,
        limit: Long,
        generatedChunkBytes: Long,
        expectedNextSequence: Long,
        expectedWraps: Long,
    ) {
        val arenaCapacity = limit - JhlogV1.ARENA_OFFSET
        assertEquals(arenaCapacity, state.arenaCapacity)
        assertTrue(state.head in 0L..arenaCapacity)
        assertTrue(state.tail in 0L..arenaCapacity)
        assertTrue(state.usedBytes in 0L..arenaCapacity)
        assertEquals(expectedNextSequence, state.nextSequence)
        assertEquals(state.nextSequence - state.firstSequence, state.chunkCount)
        assertEquals(state.firstSequence, state.evictedChunks)
        assertEquals(generatedChunkBytes - state.usedBytes, state.evictedBytes)
        assertEquals(generatedChunkBytes, state.generatedChunkBytes)
        assertEquals(expectedWraps, state.wraps)
        assertTrue(state.retainedBytes <= limit)
        assertEquals(state.retainedBytes, file.length())
        assertEquals(1L + expectedNextSequence + expectedWraps, state.generation)
    }

    private fun readLogicalChunks(file: File, state: V1State): List<LogicalChunk> {
        val chunks = ArrayList<LogicalChunk>(state.chunkCount.toInt())
        RandomAccessFile(file, "r").use { access ->
            var position = state.head
            var expectedSequence = state.firstSequence
            repeat(state.chunkCount.toInt()) {
                if (
                    position + JhlogV1.CHUNK_HEADER_BYTES > state.arenaCapacity ||
                    JhlogV1.ARENA_OFFSET + position + JhlogV1.CHUNK_HEADER_BYTES > access.length()
                ) {
                    position = 0L
                }
                var offset = JhlogV1.ARENA_OFFSET + position
                var header = readAt(access, offset, JhlogV1.CHUNK_HEADER_BYTES)
                if (!header.startsWith(JhlogV1.CHUNK_MAGIC) && position != 0L) {
                    position = 0L
                    offset = JhlogV1.ARENA_OFFSET
                    header = readAt(access, offset, JhlogV1.CHUNK_HEADER_BYTES)
                }
                assertTrue(header.startsWith(JhlogV1.CHUNK_MAGIC))
                assertEquals(JhlogV1.CHUNK_HEADER_BYTES, uint16(header, 4))
                assertEquals(crc32(header, 0, 32), uint32(header, 32))
                val sequence = uint64(header, 8)
                assertEquals(expectedSequence, sequence)
                val storedSize = uint32(header, 16).toInt()
                val rawSize = uint32(header, 20).toInt()
                val rawCrc = uint32(header, 28)
                val payload = readAt(access, offset + JhlogV1.CHUNK_HEADER_BYTES, storedSize)
                val trailer = readAt(
                    access,
                    offset + JhlogV1.CHUNK_HEADER_BYTES + storedSize,
                    JhlogV1.COMMIT_TRAILER_BYTES,
                )
                assertTrue(trailer.startsWith(JhlogV1.COMMIT_MAGIC))
                assertEquals(sequence, uint64(trailer, 4))
                assertEquals(storedSize.toLong(), uint32(trailer, 12))
                assertEquals(rawSize.toLong(), uint32(trailer, 16))
                assertEquals(rawCrc, uint32(trailer, 20))
                chunks += LogicalChunk(sequence, rawCrc, payload)
                val total = JhlogV1.CHUNK_HEADER_BYTES + storedSize + JhlogV1.COMMIT_TRAILER_BYTES
                position += total
                if (position >= state.arenaCapacity) position = 0L
                expectedSequence++
            }
        }
        return chunks
    }

    private fun readLatestState(file: File): V1State {
        RandomAccessFile(file, "r").use { access ->
            val candidates = listOfNotNull(
                decodeState(readAt(access, JhlogV1.SUPERBLOCK_A_OFFSET, JhlogV1.SUPERBLOCK_BYTES.toInt())),
                decodeState(readAt(access, JhlogV1.SUPERBLOCK_B_OFFSET, JhlogV1.SUPERBLOCK_BYTES.toInt())),
            )
            return requireNotNull(candidates.maxByOrNull(V1State::generation))
        }
    }

    private fun decodeState(raw: ByteArray): V1State? {
        if (!raw.startsWith(JhlogV1.SUPERBLOCK_MAGIC)) return null
        if (uint16(raw, 4) != JhlogV1.SUPERBLOCK_SCHEMA) return null
        if (uint32(raw, raw.size - Int.SIZE_BYTES) != crc32(raw, 0, raw.size - Int.SIZE_BYTES)) return null
        return V1State(
            generation = uint64(raw, 8),
            arenaCapacity = uint64(raw, 16),
            head = uint64(raw, 24),
            tail = uint64(raw, 32),
            usedBytes = uint64(raw, 40),
            firstSequence = uint64(raw, 48),
            nextSequence = uint64(raw, 56),
            chunkCount = uint64(raw, 64),
            wraps = uint64(raw, 72),
            evictedChunks = uint64(raw, 80),
            evictedBytes = uint64(raw, 88),
            generatedChunkBytes = uint64(raw, 96),
            retainedBytes = uint64(raw, 104),
        )
    }

    private fun livePayload(generation: Long, fill: Int): ByteArray =
        ByteArray(LogGrowthWire.LIVE_BYTES) { fill.toByte() }.also { payload ->
            repeat(Long.SIZE_BYTES) { index ->
                payload[8 + index] = (generation ushr (index * Byte.SIZE_BITS)).toByte()
            }
        }

    private fun deterministicPayload(sequence: Int, size: Int): ByteArray =
        ByteArray(size) { index ->
            val mixed = sequence * 0x45d9f3b + index * 0x119de1f3
            (mixed xor (mixed ushr 16)).toByte()
        }

    private fun readBytes(file: File, offset: Long, size: Int): ByteArray =
        RandomAccessFile(file, "r").use { access -> readAt(access, offset, size) }

    private fun readAt(access: RandomAccessFile, offset: Long, size: Int): ByteArray =
        ByteArray(size).also { bytes ->
            access.seek(offset)
            access.readFully(bytes)
        }

    private fun crc32(bytes: ByteArray): Long = crc32(bytes, 0, bytes.size)

    private fun crc32(bytes: ByteArray, offset: Int, length: Int): Long = CRC32().run {
        update(bytes, offset, length)
        value
    }

    private fun uint16(bytes: ByteArray, offset: Int): Int =
        (bytes[offset].toInt() and 0xff) or ((bytes[offset + 1].toInt() and 0xff) shl Byte.SIZE_BITS)

    private fun uint32(bytes: ByteArray, offset: Int): Long {
        var value = 0L
        repeat(Int.SIZE_BYTES) { index ->
            value = value or ((bytes[offset + index].toLong() and 0xffL) shl (index * Byte.SIZE_BITS))
        }
        return value
    }

    private fun uint64(bytes: ByteArray, offset: Int): Long {
        var value = 0L
        repeat(Long.SIZE_BYTES) { index ->
            value = value or ((bytes[offset + index].toLong() and 0xffL) shl (index * Byte.SIZE_BITS))
        }
        return value
    }

    private fun ByteArray.startsWith(prefix: ByteArray): Boolean {
        if (size < prefix.size) return false
        return prefix.indices.all { index -> this[index] == prefix[index] }
    }

    private data class V1State(
        val generation: Long,
        val arenaCapacity: Long,
        val head: Long,
        val tail: Long,
        val usedBytes: Long,
        val firstSequence: Long,
        val nextSequence: Long,
        val chunkCount: Long,
        val wraps: Long,
        val evictedChunks: Long,
        val evictedBytes: Long,
        val generatedChunkBytes: Long,
        val retainedBytes: Long,
    )

    private data class LogicalChunk(
        val sequence: Long,
        val rawCrc: Long,
        val payload: ByteArray,
    )

    private class FaultInjectingWriter(
        file: File,
    ) : JankHunterRandomAccessBinaryWriter {
        private val access = RandomAccessFile(file, "rw")
        private var targetWrite = Int.MAX_VALUE
        private var writesAfterArming = 0

        override val path: String = file.absolutePath

        fun failPartiallyOnWrite(number: Int) {
            require(number > 0)
            targetWrite = number
            writesAfterArming = 0
        }

        override fun sizeBytes(): Long = access.length()

        override fun readBytes(position: Long, target: ByteArray, offset: Int, length: Int) {
            access.seek(position)
            access.readFully(target, offset, length)
        }

        override fun writeBytes(position: Long, source: ByteArray, offset: Int, length: Int) {
            writesAfterArming++
            access.seek(position)
            if (writesAfterArming == targetWrite) {
                val partial = length / 2
                if (partial > 0) access.write(source, offset, partial)
                throw IOException("injected partial write $targetWrite")
            }
            access.write(source, offset, length)
        }

        override fun truncate(size: Long) = access.setLength(size)

        override fun flush() = access.fd.sync()

        override fun close() = access.close()
    }
}
