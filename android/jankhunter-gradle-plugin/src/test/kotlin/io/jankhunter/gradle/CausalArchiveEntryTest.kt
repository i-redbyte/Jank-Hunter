package io.jankhunter.gradle

import java.io.ByteArrayOutputStream
import java.io.InputStream
import java.util.zip.ZipInputStream
import java.util.zip.ZipOutputStream
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class CausalArchiveEntryTest {
    @Test fun largeResourceIsWrittenBeforeItsTailIsReadAndClosedOnce() {
        val archive = ByteArrayOutputStream()
        var delivered = 0
        var consumed = 0
        var closed = 0
        val length = 2 * 1024 * 1024
        val input = object : InputStream() {
            override fun read(): Int = error("bulk reads required")
            override fun read(buffer: ByteArray, offset: Int, count: Int): Int {
                assertTrue("resource was materialized before output", consumed - delivered <= 16 * 1024)
                if (consumed == length) return -1
                val size = minOf(count, length - consumed)
                buffer.fill(42, offset, offset + size)
                consumed += size
                return size
            }
            override fun close() { closed++ }
        }
        object : ZipOutputStream(archive) {
            override fun write(buffer: ByteArray, offset: Int, count: Int) {
                delivered += count
                super.write(buffer, offset, count)
            }
        }.use { writeCausalArchiveEntry(it, "assets/model.bin", { input }) }
        assertEquals(length, delivered)
        assertEquals(1, closed)
        ZipInputStream(archive.toByteArray().inputStream()).use {
            assertEquals("assets/model.bin", it.nextEntry.name)
            val bytes = it.readBytes()
            assertEquals(length, bytes.size)
            assertTrue(bytes.all { byte -> byte == 42.toByte() })
        }
    }
}
