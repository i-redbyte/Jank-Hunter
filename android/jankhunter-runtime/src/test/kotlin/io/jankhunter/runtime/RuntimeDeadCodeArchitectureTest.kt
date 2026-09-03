package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.BinaryLogWriter
import io.jankhunter.runtime.internal.io.ColumnarMicroPage
import io.jankhunter.runtime.internal.io.RuntimeNumericColumnCodec
import java.lang.reflect.Modifier
import org.junit.Assert.assertEquals
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
        val dictionaryType = Class.forName("io.jankhunter.runtime.internal.io.DictionaryIds")
        assertTrue(dictionaryType.declaredMethods.none { method -> method.name == "definition" })
        assertTrue(dictionaryType.declaredFields.none { field -> field.name == "definitionsById" })
        assertTrue(Class.forName("io.jankhunter.runtime.internal.io.Utf8PrefixKt")
            .declaredMethods.none { method ->
                method.name == "utf8Prefix" && method.parameterTypes.firstOrNull() == String::class.java
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
    fun binaryWriterDelegatesDomainEncodingToFocusedComponents() {
        val componentTypes = BinaryLogWriter::class.java.declaredFields.mapTo(HashSet()) { field ->
            field.type.name
        }
        assertTrue("io.jankhunter.runtime.internal.io.DatabaseBinaryRecordEncoder" in componentTypes)
        assertTrue("io.jankhunter.runtime.internal.io.NetworkBinaryRecordEncoder" in componentTypes)
        assertTrue("io.jankhunter.runtime.internal.io.RuntimeCallBinaryRecordEncoder" in componentTypes)
    }

    @Test
    fun binaryWriterUsesDisjointSemanticDictionaryAndControlPaths() {
        val sinkMethods = Class.forName("io.jankhunter.runtime.internal.io.BinaryEncodingSink")
            .declaredMethods
            .mapTo(HashSet()) { method -> method.name }
        assertTrue("emitSemantic" in sinkMethods)
        assertTrue("emitDictionaryDefinition" in sinkMethods)
        assertTrue("emitControl" in sinkMethods)
        assertTrue("emit" !in sinkMethods)

        val writerMethods = BinaryLogWriter::class.java.declaredMethods
            .mapTo(HashSet()) { method -> method.name }
        assertTrue("recordSemantic" in writerMethods)
        assertTrue("recordDictionary" in writerMethods)
        assertTrue("recordControl" in writerMethods)
        assertTrue("record" !in writerMethods)
    }

    @Test
    fun asyncWriterDelegatesRetentionPolicy() {
        val consumerType = Class.forName("io.jankhunter.runtime.internal.io.AsyncWriterConsumer")
        assertTrue(consumerType.declaredFields.any { field ->
            field.type.name == "io.jankhunter.runtime.internal.io.SessionLogRetentionCoordinator"
        })
    }

    @Test
    fun asyncWriterDelegatesControlAdmissionAndWaiting() {
        val writerType = Class.forName("io.jankhunter.runtime.internal.io.AsyncLogWriter")
        assertTrue(writerType.declaredFields.any { field ->
            field.type.name == "io.jankhunter.runtime.internal.io.AsyncWriterControlCoordinator"
        })
    }

    @Test
    fun asynchronousPipelinesOwnExplicitProducerConsumerAndLifecycleRoles() {
        val asyncFields = Class.forName("io.jankhunter.runtime.internal.io.AsyncLogWriter")
            .declaredFields
            .associateBy({ field -> field.name }, { field -> field.type.simpleName })
        assertEquals("AsyncWriterProducer", asyncFields["producer"])
        assertEquals("AsyncWriterConsumer", asyncFields["consumer"])
        assertEquals("AsyncWriterLifecycle", asyncFields["lifecycle"])
        assertTrue("queuedEvents" !in asyncFields)
        assertTrue("admissionLock" !in asyncFields)
        assertTrue("worker" !in asyncFields)

        val graphFields = RuntimeCallGraph::class.java.declaredFields
            .associateBy({ field -> field.name }, { field -> field.type.simpleName })
        assertEquals("RuntimeGraphProducer", graphFields["producer"])
        assertEquals("RuntimeGraphConsumer", graphFields["consumer"])
        assertEquals("RuntimeGraphLifecycle", graphFields["lifecycle"])
        assertTrue("threadState" !in graphFields)
        assertTrue("producers" !in graphFields)
        assertTrue("activeWriter" !in graphFields)
    }

    @Test
    fun asynchronousRoleDependenciesStayDirectional() {
        assertFieldTypesExclude(
            typeName = "io.jankhunter.runtime.internal.io.AsyncWriterProducer",
            forbiddenFragments = setOf("File", "BinaryLogWriter", "SessionLog", "JankHunterBinaryStorage"),
        )
        assertFieldTypesExclude(
            typeName = "io.jankhunter.runtime.internal.io.AsyncWriterConsumer",
            forbiddenFragments = setOf("Semaphore", "ReentrantLock", "AsyncEventLanes", "PendingDatabase"),
        )
        assertFieldTypesExclude(
            typeName = "io.jankhunter.runtime.RuntimeGraphProducer",
            forbiddenFragments = setOf("AsyncLogWriter", "RuntimeCallBatchPool", "AtomicBoolean"),
        )
        assertFieldTypesExclude(
            typeName = "io.jankhunter.runtime.RuntimeGraphConsumer",
            forbiddenFragments = setOf("ThreadLocal", "ConcurrentLinkedQueue", "AsyncLogWriter"),
        )
    }

    @Test
    fun asynchronousHotPathsDoNotUseRoleForwardingAccessors() {
        val asyncForwarders = setOf(
            "getQueuedEvents",
            "getAdmissionLock",
            "getProducerContext",
            "getEventLanes",
            "getDatabaseEventPool",
            "getDatabaseTransactionEventPool",
            "getRuntimeCallsEventPool",
            "getStableCountersEventPool",
            "getAcceptedSequence",
            "getBinaryStorage",
            "getCompletedSequence",
            "getWriter",
            "getWorker",
        )
        val graphForwarders = setOf(
            "getThreadState",
            "getProducers",
            "getEpoch",
            "getRunning",
            "getConsumerFailed",
            "getProducerWake",
            "getBatchPool",
        )
        val asyncMethods = Class.forName("io.jankhunter.runtime.internal.io.AsyncLogWriter")
            .declaredMethods
            .mapTo(HashSet(), java.lang.reflect.Method::getName)
        val graphMethods = RuntimeCallGraph::class.java.declaredMethods
            .mapTo(HashSet(), java.lang.reflect.Method::getName)

        assertTrue("Async writer role forwarders remain: ${asyncMethods intersect asyncForwarders}",
            asyncMethods.none(asyncForwarders::contains))
        assertTrue("Runtime graph role forwarders remain: ${graphMethods intersect graphForwarders}",
            graphMethods.none(graphForwarders::contains))
    }

    @Test
    fun microPageRetainsEncodedEntropySectionsInsteadOfEncodingTwice() {
        val fields = ColumnarMicroPage::class.java.declaredFields.mapTo(HashSet()) { field -> field.name }

        assertTrue("entropySections" in fields)
        assertTrue("entropySection" !in fields)
    }

    @Test
    fun gzipProductionContractDoesNotRequireRansSections() {
        val jhlog = Class.forName("io.jankhunter.runtime.internal.io.Jhlog")
        val required = jhlog.getDeclaredField("REQUIRED_FEATURES").getLong(null)
        val rans = jhlog.getDeclaredField("FEATURE_RANS_MICRO_PAGE_SECTIONS").getLong(null)

        assertTrue(required and (1L shl 22) == 0L)
        assertEquals(1L shl 5, rans)
    }

    @Test
    fun numericColumnCodecsShareSpecializedBitPackingPrimitives() {
        assertTrue(runCatching {
            Class.forName("io.jankhunter.runtime.internal.io.PackedLongs")
        }.isSuccess)
        assertTrue(RuntimeNumericColumnCodec::class.java.declaredMethods.none { method ->
            method.name == "writePacked"
        })
    }

    @Test
    fun pendingWriterEventsAreOwnedByDomainUnitsInsteadOfOneNestedGodHierarchy() {
        val domainTypes = listOf(
            "PendingDatabaseEvent",
            "PendingDatabaseTransactionEvent",
            "PendingHttpEvent",
            "PendingOperationEvent",
            "PendingCounterEvent",
            "PendingSessionEvent",
        )
        domainTypes.forEach { simpleName ->
            assertTrue(
                "$simpleName must be a top-level domain event",
                runCatching {
                    Class.forName("io.jankhunter.runtime.internal.io.$simpleName")
                }.isSuccess,
            )
        }
        assertTrue(
            runCatching {
                Class.forName("io.jankhunter.runtime.internal.io.PendingLogEvent\$Database")
            }.isFailure,
        )
    }

    @Test
    fun threadConfinedBinaryWriterHotPathDoesNotEnterIntrinsicMonitors() {
        val hotMethods = setOf(
            "withProducer",
            "database",
            "databaseTransaction",
            "runtimeCalls",
            "counter",
            "gauge",
        )
        val synchronizedHotMethods = BinaryLogWriter::class.java.declaredMethods
            .filter { method -> method.name in hotMethods && Modifier.isSynchronized(method.modifiers) }
            .map { method -> method.name }
            .toSet()

        assertTrue(
            "single-writer hot path must be monitor-free: $synchronizedHotMethods",
            synchronizedHotMethods.isEmpty(),
        )
    }

    @Test
    fun runtimeHookPublicationUsesPerProducerAdmissionWithoutGlobalAtomicContention() {
        val transportFields = RuntimeHookEventTransport::class.java.declaredFields
            .mapTo(HashSet()) { field -> field.name }
        val eventBufferFields = Class.forName(
            "io.jankhunter.runtime.RuntimeHookEventTransport\$EventBuffer",
        ).declaredFields.mapTo(HashSet()) { field -> field.name }

        assertTrue("publisherState" !in transportFields)
        assertTrue("producerActive" in eventBufferFields)
    }

    @Test
    fun binaryWriterDelegatesDomainEncodingToFocusedUnits() {
        val encoderTypes = BinaryLogWriter::class.java.declaredFields
            .mapTo(HashSet()) { field -> field.type.simpleName }

        assertTrue("BinaryDictionaryEncoder" in encoderTypes)
        assertTrue("BinaryLogGrowthEncoder" in encoderTypes)
        assertTrue("SessionBinaryRecordEncoder" in encoderTypes)
        assertTrue("ExecutionBinaryRecordEncoder" in encoderTypes)
        assertTrue("MetricBinaryRecordEncoder" in encoderTypes)
        val fieldNames = BinaryLogWriter::class.java.declaredFields.mapTo(HashSet()) { field -> field.name }
        assertTrue("dictionary" !in fieldNames)
        assertTrue("stableSymbolDefinitions" !in fieldNames)
        assertTrue("previousLogGrowthHistory" !in fieldNames)
        assertTrue("previousLogGrowthLive" !in fieldNames)
    }

    private fun assertFieldTypesExclude(typeName: String, forbiddenFragments: Set<String>) {
        val dependencies = Class.forName(typeName).declaredFields.map { field -> field.type.name }
        val forbidden = dependencies.filter { dependency ->
            forbiddenFragments.any(dependency::contains)
        }
        assertTrue("$typeName has forbidden role dependencies: $forbidden", forbidden.isEmpty())
    }
}
