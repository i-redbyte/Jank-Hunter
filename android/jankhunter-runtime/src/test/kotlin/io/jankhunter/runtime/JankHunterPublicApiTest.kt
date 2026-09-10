package io.jankhunter.runtime

import io.jankhunter.runtime.internal.system.ActivityTracker
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.io.LogGrowthManager
import io.jankhunter.runtime.internal.system.FpsMonitor
import io.jankhunter.runtime.internal.system.UserRelevantSamplingSchedule
import io.jankhunter.runtime.internal.system.MainThreadWatchdog
import io.jankhunter.runtime.internal.system.MemorySampler
import io.jankhunter.runtime.internal.system.MainThreadDispatchTracker
import io.jankhunter.runtime.internal.system.MemoryPressureSampler
import io.jankhunter.runtime.internal.system.ObjectRetentionWatcher
import io.jankhunter.runtime.internal.system.ProcCpuSampler
import io.jankhunter.runtime.internal.system.RetainedHeapDumper
import io.jankhunter.runtime.internal.system.RuntimeGcStats
import io.jankhunter.runtime.internal.system.RuntimeMaintenanceScheduler
import io.jankhunter.runtime.internal.system.SystemContextSampler
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterPublicApiTest {
    @Test
    fun facadeExposesOnlyLifecycleStorageAndExportOperations() {
        val exposed = JankHunter::class.java.declaredMethods
            .asSequence()
            .filter { java.lang.reflect.Modifier.isPublic(it.modifiers) }
            .filter { java.lang.reflect.Modifier.isStatic(it.modifiers) }
            .map(java.lang.reflect.Method::getName)
            .filterNot { '$' in it }
            .toSortedSet()

        assertEquals(
            sortedSetOf(
                "autoInit",
                "captureLogArchive",
                "captureLogSnapshot",
                "flush",
                "init",
                "initDiagnostics",
                "isRuntimeEnabled",
                "isStarted",
                "reconfigure",
                "setRuntimeEnabled",
                "shutdown",
                "switchBinaryStorage",
            ),
            exposed,
        )
    }

    @Test
    fun facadeOwnsOnlyItsCompositionRoot() {
        val stateFields = JankHunter::class.java.declaredFields
            .asSequence()
            .filterNot { it.type.isPrimitive }
            .map(java.lang.reflect.Field::getName)
            .filterNot { it == "INSTANCE" }
            .toSet()

        assertTrue("JankHunter owns runtime services directly: $stateFields", stateFields == setOf("runtime"))
    }

    @Test
    fun obsoleteHandlerProxyApiIsNotExposed() {
        val obsoleteNames = setOf(
            "postHandlerRunnable",
            "postHandlerRunnableAtFront",
            "postHandlerRunnableDelayed",
            "postHandlerRunnableAtTime",
            "removeHandlerCallbacks",
            "removeHandlerCallbacksAndMessages",
            "hasHandlerCallbacks",
        )

        val exposed = JankHunter::class.java.declaredMethods
            .asSequence()
            .mapTo(HashSet(), java.lang.reflect.Method::getName)

        assertTrue("Obsolete Handler proxies remain exposed: ${obsoleteNames intersect exposed}",
            exposed.none(obsoleteNames::contains))
    }

    @Test
    fun asynchronousDecoratorsRequireInjectedCallbacks() {
        val decorators = listOf(
            JankHunterRunnable::class.java,
            JankHunterCallable::class.java,
            JankHunterCoroutineFunction2::class.java,
            JankHunterClickListener::class.java,
            JankHunterHandlerRunnable::class.java,
            JankHunterExecutor::class.java,
            JankHunterExecutorService::class.java,
            JankHunterScheduledExecutorService::class.java,
        )

        decorators.forEach { decorator ->
            assertTrue(
                "${decorator.simpleName} still depends on the global runtime facade",
                decorator.declaredConstructors.any { constructor ->
                    constructor.parameterTypes.any(RuntimeAsyncCallbacks::class.java::isAssignableFrom)
                },
            )
        }
    }

    @Test
    fun asynchronousDecoratorsUsePrimitiveClockPort() {
        val decorators = listOf(
            JankHunterExecutor::class.java,
            JankHunterExecutorService::class.java,
            JankHunterScheduledExecutorService::class.java,
        )
        decorators.forEach { decorator ->
            assertTrue(
                "${decorator.simpleName} still boxes clock values",
                decorator.declaredConstructors.any { constructor ->
                    constructor.parameterTypes.any(RuntimeLongSource::class.java::isAssignableFrom)
                },
            )
        }
        RuntimeAsyncCallbacks::class.java.declaredMethods
            .filter { it.name == "runExecutorTask" || it.name == "callExecutorTask" }
            .forEach { method ->
                assertTrue(
                    "${method.name} still accepts a boxing clock",
                    method.parameterTypes.any(RuntimeLongSource::class.java::isAssignableFrom),
                )
            }
    }

    @Test
    fun coreRuntimeServicesUsePrimitiveClockPort() {
        val clockedServices = listOf(
            RuntimeComponentGraph::class.java,
            RuntimeCoordinator::class.java,
            RuntimeRetentionTelemetry::class.java,
            RuntimeMetricsService::class.java,
            RuntimeOperationTelemetry::class.java,
            RuntimeSessionController::class.java,
            RuntimeTelemetryAccess::class.java,
            RuntimeLifecycleController::class.java,
            RuntimeSamplingService::class.java,
            RuntimeContextTelemetry::class.java,
        )

        clockedServices.forEach { service ->
            assertTrue(
                "${service.simpleName} still boxes clock values",
                service.declaredConstructors.any { constructor ->
                    constructor.parameterTypes.any(RuntimeLongSource::class.java::isAssignableFrom)
                },
            )
        }
    }

    @Test
    fun systemCollectorsUsePrimitiveValuePorts() {
        val longSources = listOf(
            ObjectRetentionWatcher::class.java,
            MainThreadDispatchTracker::class.java,
            RuntimeGcStats::class.java,
            RetainedHeapDumper::class.java,
        )
        longSources.forEach { collector ->
            assertTrue(
                "${collector.simpleName} still boxes long values",
                collector.declaredConstructors.any { constructor ->
                    constructor.parameterTypes.any(RuntimeLongSource::class.java::isAssignableFrom)
                },
            )
        }
        val recurringTask = RuntimeMaintenanceScheduler::class.java.declaredClasses
            .single { nested -> nested.simpleName == "RecurringTask" }
        assertTrue(recurringTask.declaredConstructors.any { constructor ->
            constructor.parameterTypes.any(RuntimeLongSource::class.java::isAssignableFrom)
        })
        assertTrue(ProcCpuSampler::class.java.declaredConstructors.any { constructor ->
            constructor.parameterTypes.any(RuntimeIntSource::class.java::isAssignableFrom)
        })
        assertTrue(MemoryPressureSampler::class.java.declaredConstructors.any { constructor ->
            constructor.parameterTypes.any(RuntimeIntSource::class.java::isAssignableFrom)
        })
        listOf(
            MemorySampler::class.java,
            SystemContextSampler::class.java,
            UserRelevantSamplingSchedule::class.java,
        ).forEach { collector ->
            assertTrue(collector.declaredConstructors.any { constructor ->
                constructor.parameterTypes.any(RuntimeBooleanSource::class.java::isAssignableFrom)
            })
        }
    }

    @Test
    fun writerClockSourcesArePrimitive() {
        listOf(AsyncLogWriter::class.java, LogGrowthManager::class.java).forEach { writer ->
            assertTrue(
                "${writer.simpleName} still boxes wall-clock values",
                writer.declaredConstructors.any { constructor ->
                    constructor.parameterTypes.any(RuntimeLongSource::class.java::isAssignableFrom)
                },
            )
        }
    }

    @Test
    fun asynchronousWriterConstructionIsOwnedByFactory() {
        assertTrue(AsyncLogWriter::class.java.declaredMethods.none { method ->
            method.name == "open"
        })
        assertTrue(AsyncLogWriterFactory::class.java.declaredMethods.any { method ->
            method.name == "open"
        })
        assertTrue(RuntimeSessionController::class.java.declaredConstructors.any { constructor ->
            constructor.parameterTypes.any(AsyncLogWriterFactory::class.java::isAssignableFrom)
        })
    }

    @Test
    fun asynchronousWriterLifecycleStateIsEncapsulated() {
        val lifecycleType = Class.forName("io.jankhunter.runtime.internal.io.AsyncWriterLifecycle")
        val fields = AsyncLogWriter::class.java.declaredFields
        assertTrue(fields.any { field -> field.type == lifecycleType })
        assertTrue(fields.none { field ->
            field.type == java.util.concurrent.atomic.AtomicBoolean::class.java ||
                field.type == java.util.concurrent.atomic.AtomicInteger::class.java
        })
    }

    @Test
    fun asynchronousWriterTerminalObserverUsesPrimitiveReasonPort() {
        val observer = AsyncLogWriter::class.java.getDeclaredField("onTerminalStop")

        assertTrue(observer.type != Function3::class.java)
    }

    @Test
    fun binaryRecordEnvelopeStateIsOwnedByEncoder() {
        val encoderType = Class.forName("io.jankhunter.runtime.internal.io.BinaryRecordEncoder")
        val fields = Class.forName("io.jankhunter.runtime.internal.io.BinaryLogWriter").declaredFields
        assertTrue(fields.any { field -> field.type == encoderType })
        val obsoleteFields = setOf("recordBody", "encodedRecord", "lastTimedRecordUs", "lastContext")
        assertTrue(fields.none { field -> field.name in obsoleteFields })
    }

    @Test
    fun systemCollectorsRequireInjectedCallbacks() {
        val callbackType = Class.forName("io.jankhunter.runtime.RuntimeCollectorCallbacks")
        val collectors = listOf(
            ActivityTracker::class.java,
            FpsMonitor::class.java,
            MainThreadWatchdog::class.java,
            MemorySampler::class.java,
            SystemContextSampler::class.java,
        )

        collectors.forEach { collector ->
            assertTrue(
                "${collector.simpleName} still depends on the global runtime facade",
                collector.declaredConstructors.any { constructor ->
                    constructor.parameterTypes.any(callbackType::isAssignableFrom)
                },
            )
        }
    }

    @Test
    fun facadeContainsNoTelemetryProxyMethods() {
        val obsoleteProxies = setOf(
            "captureMainThreadStallContext",
            "currentOwner",
            "currentScreen",
            "dumpWatchedRetainedHeap",
            "effectiveRetainedHolder",
            "ioTracingEnabled",
            "isAppForegroundForSampling",
            "isUiVisible",
            "isUserRelevantForSampling",
            "lastInitFailure",
            "logGrowthSummary",
            "recordAutomaticIO",
            "recordContext",
            "recordCounter",
            "recordGauge",
            "recordIOInternal",
            "recordLogSpam",
            "recordMainThreadDispatch",
            "recordMainThreadStall",
            "recordMemory",
            "recordProcessExit",
            "recordQuality",
            "recordRetained",
            "recordStall",
            "recordUiWindow",
            "recordWatchedRetained",
            "requestFlush",
            "setAppForeground",
            "setUiVisible",
            "setScreen",
            "startOperation",
            "watchActivity",
            "watchCloseable",
            "watchDialog",
            "watchFragment",
            "watchService",
            "watchView",
            "watchViewModel",
            "writeLogGrowthSummary",
        )
        val actual = JankHunter::class.java.declaredMethods
            .mapTo(HashSet()) { it.name.substringBefore('$') }

        assertTrue("Telemetry proxies remain on JankHunter: ${actual intersect obsoleteProxies}",
            actual.none(obsoleteProxies::contains))
    }
}
