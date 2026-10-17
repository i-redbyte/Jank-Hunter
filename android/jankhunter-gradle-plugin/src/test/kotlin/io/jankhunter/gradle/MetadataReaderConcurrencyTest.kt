package io.jankhunter.gradle

import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class MetadataReaderConcurrencyTest {
    private val descriptor = MetadataReaderDescriptor("test", setOf("2.3"), (1..5).toSet(), 255, 1, 10,
        mapOf("reader.jar" to "a".repeat(64)))
    private val metadata = kotlin.metadata.jvm.Metadata(kind = 1, metadataVersion = intArrayOf(2, 3, 0))

    @Test fun independentDecodesOverlapWhileInitializationRunsOnce() {
        val factories = AtomicInteger()
        val decoding = CountDownLatch(2)
        val release = CountDownLatch(1)
        val backend = object : MetadataReaderBackend {
            override fun decode(metadata: kotlin.Metadata): Map<*, *> {
                decoding.countDown()
                check(release.await(5, TimeUnit.SECONDS))
                return response()
            }
            override fun close() = Unit
        }
        val registry = MetadataReaderRegistry({ MetadataReaderCapabilities(listOf(descriptor)) }) {
            factories.incrementAndGet(); backend
        }
        val workers = Executors.newFixedThreadPool(2)
        try {
            val results = (1..2).map { workers.submit<MetadataReaderResult> { registry.read(metadata) } }
            assertTrue("Decode must not hold the initialization monitor", decoding.await(5, TimeUnit.SECONDS))
            release.countDown()
            results.forEach { assertEquals(KotlinMetadataAnalysisStatus.READABLE, it.get(5, TimeUnit.SECONDS).status) }
            assertEquals(1, factories.get())
        } finally {
            release.countDown(); workers.shutdownNow(); workers.awaitTermination(5, TimeUnit.SECONDS); registry.close()
        }
    }

    @Test fun transientOpenFailureDoesNotPoisonBackendSession() {
        val attempts = AtomicInteger()
        val registry = MetadataReaderRegistry({ MetadataReaderCapabilities(listOf(descriptor)) }) {
            attempts.incrementAndGet()
            if (attempts.get() == 1) throw IllegalStateException("temporary setup failure")
            object : MetadataReaderBackend {
                override fun decode(metadata: kotlin.Metadata): Map<*, *> = response()
                override fun close() = Unit
            }
        }
        try {
            assertEquals(KotlinMetadataAnalysisStatus.READER_UNAVAILABLE, registry.read(metadata).status)
            assertEquals(KotlinMetadataAnalysisStatus.READABLE, registry.read(metadata).status)
            assertEquals(2, attempts.get())
        } finally { registry.close() }
    }

    @Test fun unknownFormatsNeverLoadAReaderAndFailedBridgeIsNotReadable() {
        val factories = AtomicInteger()
        val registry = MetadataReaderRegistry({ MetadataReaderCapabilities(listOf(descriptor)) }) {
            factories.incrementAndGet()
            object : MetadataReaderBackend {
                override fun decode(metadata: kotlin.Metadata): Map<*, *> = mapOf("protocol" to 99)
                override fun close() = Unit
            }
        }
        try {
            assertEquals(KotlinMetadataAnalysisStatus.UNSUPPORTED_VERSION,
                registry.read(kotlin.metadata.jvm.Metadata(kind = 1, metadataVersion = intArrayOf(99, 0))).status)
            assertEquals(0, factories.get())
            assertEquals(KotlinMetadataAnalysisStatus.READER_UNAVAILABLE, registry.read(metadata).status)
        } finally { registry.close() }
    }

    @Test fun closeWaitsForInFlightDecodeAndResourcesAreClosedOnce() {
        val decoding = CountDownLatch(1)
        val release = CountDownLatch(1)
        val closing = CountDownLatch(1)
        val closed = AtomicInteger()
        val registry = MetadataReaderRegistry({ MetadataReaderCapabilities(listOf(descriptor)) }) {
            object : MetadataReaderBackend {
                override fun decode(metadata: kotlin.Metadata): Map<*, *> {
                    decoding.countDown(); check(release.await(5, TimeUnit.SECONDS))
                    check(closed.get() == 0); return response()
                }
                override fun close() { closed.incrementAndGet() }
            }
        }
        val workers = Executors.newFixedThreadPool(2)
        try {
            val result = workers.submit<MetadataReaderResult> { registry.read(metadata) }
            assertTrue(decoding.await(5, TimeUnit.SECONDS))
            val close = workers.submit { closing.countDown(); registry.close() }
            assertTrue(closing.await(5, TimeUnit.SECONDS))
            assertEquals(0, closed.get())
            release.countDown()
            assertEquals(KotlinMetadataAnalysisStatus.READABLE, result.get(5, TimeUnit.SECONDS).status)
            close.get(5, TimeUnit.SECONDS)
            registry.close()
            assertEquals(1, closed.get())
            assertEquals(KotlinMetadataAnalysisStatus.READER_UNAVAILABLE, registry.read(metadata).status)
        } finally { release.countDown(); workers.shutdownNow(); workers.awaitTermination(5, TimeUnit.SECONDS); registry.close() }
    }

    @Test fun parallelInitializationCannotExceedEightBackends() {
        val descriptors = (0..9).map { index -> descriptor.copy(id = "limit-$index", families = setOf("2.$index")) }
        val inputs = (0..9).map { index -> kotlin.metadata.jvm.Metadata(kind = 1, metadataVersion = intArrayOf(2, index)) }
        val opened = AtomicInteger()
        val closed = AtomicInteger()
        val registry = MetadataReaderRegistry({ MetadataReaderCapabilities(descriptors) }) {
            opened.incrementAndGet()
            object : MetadataReaderBackend {
                override fun decode(metadata: kotlin.Metadata): Map<*, *> = response()
                override fun close() { closed.incrementAndGet() }
            }
        }
        val start = CountDownLatch(1)
        val workers = Executors.newFixedThreadPool(10)
        try {
            val futures = inputs.map { input -> workers.submit<MetadataReaderResult> {
                check(start.await(5, TimeUnit.SECONDS)); registry.read(input)
            } }
            start.countDown()
            val results = futures.map { it.get(5, TimeUnit.SECONDS) }
            assertEquals(8, results.count { it.status == KotlinMetadataAnalysisStatus.READABLE })
            assertEquals(2, results.count { it.status == KotlinMetadataAnalysisStatus.READER_UNAVAILABLE })
            assertEquals(8, opened.get())
            val available = results.indexOfFirst { it.status == KotlinMetadataAnalysisStatus.READABLE }
            assertEquals(KotlinMetadataAnalysisStatus.READABLE, registry.read(inputs[available]).status)
            assertEquals(8, opened.get())
        } finally {
            start.countDown(); workers.shutdownNow(); workers.awaitTermination(5, TimeUnit.SECONDS); registry.close()
        }
        assertEquals(8, closed.get())
        registry.close()
        assertEquals(8, closed.get())
    }

    @Test fun interruptedInitializationAndDecodePreserveCancellationWithoutPoisoningTheReader() {
        for (duringOpen in listOf(true, false)) {
            val opened = AtomicInteger()
            var first = true
            val registry = MetadataReaderRegistry({ MetadataReaderCapabilities(listOf(descriptor)) }) {
                opened.incrementAndGet()
                if (duringOpen && first) { first = false; throw InterruptedException("cancelled setup") }
                object : MetadataReaderBackend {
                    override fun decode(metadata: kotlin.Metadata): Map<*, *> {
                        if (!duringOpen && first) { first = false; throw InterruptedException("cancelled decode") }
                        return response()
                    }
                    override fun close() = Unit
                }
            }
            val alreadyInterrupted = Thread.interrupted()
            try {
                assertEquals(KotlinMetadataAnalysisStatus.READER_UNAVAILABLE, registry.read(metadata).status)
                assertTrue("Cancellation flag must be restored", Thread.interrupted())
                assertEquals(KotlinMetadataAnalysisStatus.READABLE, registry.read(metadata).status)
                assertEquals(if (duringOpen) 2 else 1, opened.get())
            } finally {
                Thread.interrupted(); registry.close()
                if (alreadyInterrupted) Thread.currentThread().interrupt()
            }
        }
    }

    private fun response(): Map<*, *> = mapOf("protocol" to 1, "status" to "READABLE",
        "generated" to emptyMap<String, String>(), "declared" to emptySet<String>(),
        "delegates" to emptyMap<String, String?>(), "known" to true, "synthetic" to false)
}
