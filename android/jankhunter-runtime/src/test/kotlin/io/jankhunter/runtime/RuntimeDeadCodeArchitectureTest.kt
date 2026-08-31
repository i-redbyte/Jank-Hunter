package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.BinaryLogWriter
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeDeadCodeArchitectureTest {
    @Test
    fun runtimeServicesExposeNoObsoleteSpecializedRetentionOrSingleEventProxies() {
        val obsoleteRetentionMethods = setOf(
            "watchCloseable",
            "watchDialog",
            "watchFragment",
            "watchService",
            "watchView",
            "watchViewModel",
        )
        val retentionMethods = RuntimeRetentionTelemetry::class.java.declaredMethods
            .mapTo(HashSet()) { method -> method.name }

        assertTrue(retentionMethods.none(obsoleteRetentionMethods::contains))
        assertTrue(BinaryLogWriter::class.java.declaredMethods.none { method -> method.name == "runtimeCall" })
        assertTrue(RuntimeCallGraph::class.java.declaredMethods.none { method ->
            method.name == "fullyAccountedEventsForTest"
        })
    }

    @Test
    fun obsoleteWorkerConveniencesAndUnusedCachesAreAbsent() {
        val obsoleteWorkerMethods = setOf("record", "trace", "traceSuspending")
        assertTrue(RuntimeWorkerTelemetry::class.java.declaredMethods.none { method ->
            method.name in obsoleteWorkerMethods
        })
        assertTrue(runCatching {
            Class.forName("io.jankhunter.runtime.internal.io.BitLruCache")
        }.isFailure)
        assertTrue(runCatching {
            Class.forName("io.jankhunter.runtime.internal.io.ChunkDictionaryUsage")
        }.isFailure)
        val atomicMethods = Class.forName("io.jankhunter.runtime.RuntimeAtomicsKt")
            .declaredMethods
            .map(java.lang.reflect.Method::getName)
        assertTrue("registerFirstAvailable" !in atomicMethods)
        assertTrue(RuntimeHookFailureTracker::class.java.declaredMethods.none { method ->
            method.name == "snapshot"
        })
    }

    @Test
    fun runtimeStateDoesNotDuplicateWriterOwnedLogGrowthManager() {
        assertTrue(RuntimeState::class.java.declaredFields.none { field ->
            field.name == "logGrowthManager"
        })
        val writerType = Class.forName("io.jankhunter.runtime.internal.io.AsyncLogWriter")
        assertTrue(writerType.declaredMethods.none { method ->
            method.name == "logGrowthManager"
        })
    }

    @Test
    fun binaryWriterDelegatesDatabaseEncodingToADomainComponent() {
        val componentTypes = BinaryLogWriter::class.java.declaredFields.mapTo(HashSet()) { field ->
            field.type.name
        }
        assertTrue("io.jankhunter.runtime.internal.io.DatabaseBinaryRecordEncoder" in componentTypes)
        assertTrue("io.jankhunter.runtime.internal.io.NetworkBinaryRecordEncoder" in componentTypes)
    }

    @Test
    fun asyncWriterDelegatesRetentionPolicy() {
        val writerType = Class.forName("io.jankhunter.runtime.internal.io.AsyncLogWriter")
        assertTrue(writerType.declaredFields.any { field ->
            field.type.name == "io.jankhunter.runtime.internal.io.SessionLogRetentionCoordinator"
        })
    }
}
