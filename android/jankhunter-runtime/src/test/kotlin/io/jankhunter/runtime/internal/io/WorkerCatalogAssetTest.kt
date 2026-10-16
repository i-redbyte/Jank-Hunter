package io.jankhunter.runtime.internal.io

import java.io.ByteArrayInputStream
import java.io.FileNotFoundException
import java.io.IOException
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class WorkerCatalogAssetTest {
    @Test
    fun preservesBuildDocumentAndClosesStream() {
        var closed = false
        val data = "{\"schema\":1,\"status\":\"available\"}".toByteArray()
        val stream = object : ByteArrayInputStream(data) { override fun close() { closed = true } }
        assertArrayEquals(data, readWorkerCatalogAsset { stream })
        assertTrue(closed)
    }

    @Test
    fun missingUnreadableAndOversizedRemainDistinctAndBounded() {
        assertEquals("{\"schema\":1,\"status\":\"missing\"}", readWorkerCatalogAsset { throw FileNotFoundException() }.decodeToString())
        assertEquals("{\"schema\":1,\"status\":\"unreadable\"}", readWorkerCatalogAsset { throw IOException() }.decodeToString())
        var closed = false
        val stream = object : ByteArrayInputStream(ByteArray(128 * 1024 + 1)) { override fun close() { closed = true } }
        assertEquals("{\"schema\":1,\"status\":\"oversized\"}", readWorkerCatalogAsset { stream }.decodeToString())
        assertTrue(closed)
    }
}
