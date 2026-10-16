package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.io.CausalKind
import java.nio.file.Files
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import kotlin.coroutines.Continuation
import kotlin.coroutines.EmptyCoroutineContext
import kotlin.coroutines.intrinsics.COROUTINE_SUSPENDED
import kotlin.coroutines.jvm.internal.CoroutineStackFrame
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeCausalCoroutineTelemetryTest {
    @Test
    fun reusedDecoratedBlockGetsDistinctCompletedPlacements() = withSession(capacity = 8) { graph ->
        val registry = checkNotNull(graph.state.collectionEpochs.current?.causalRegistry)
        var delegateCalls = 0
        val delegate: Function2<Any?, Any?, Any?> = { _, _ ->
            delegateCalls++
            Unit
        }
        val decorated = checkNotNull(graph.asyncTelemetry.wrapCoroutineBlock(delegate, "test.Owner"))
        val first = checkNotNull(graph.asyncTelemetry.wrapCausalCoroutineBlock(
            decorated, 11L, "test.Owner#first()V", 10, 1,
        ))
        val second = checkNotNull(graph.asyncTelemetry.wrapCausalCoroutineBlock(
            decorated, 12L, "test.Owner#second()V", 11, 1,
        ))
        val firstJob = Any()
        val secondJob = Any()
        graph.asyncTelemetry.coroutineBuilderReturned(first, firstJob)
        graph.asyncTelemetry.coroutineBuilderReturned(second, secondJob)
        val firstId = registry.find(CausalKind.COROUTINE, firstJob)
        val secondId = registry.find(CausalKind.COROUTINE, secondJob)
        assertTrue(firstId > 0L && secondId > 0L && firstId != secondId)

        invokeBlock(first)
        assertEquals(1, delegateCalls)
        assertEquals(0L, registry.find(CausalKind.COROUTINE, firstJob))
        assertEquals(secondId, registry.find(CausalKind.COROUTINE, secondJob))
        invokeBlock(second)
        assertEquals(2, delegateCalls)
        assertEquals(0L, registry.find(CausalKind.COROUTINE, secondJob))
        assertEquals(0, registry.liveEntries())
    }

    @Test
    fun completedCoroutineReleasesJobAndLaunchAliasesWithoutGc() = withSession(capacity = 8) { graph ->
        val registry = checkNotNull(graph.state.collectionEpochs.current?.causalRegistry)
        val retainedJobs = ArrayList<Any>()
        val retainedBlocks = ArrayList<Function2<*, *, *>>()
        val block: Function2<Any?, Any?, Any?> = { _, _ -> Unit }

        repeat(100) {
            val prepared = checkNotNull(graph.asyncTelemetry.wrapCausalCoroutineBlock(
                block, 1L, "test.Owner#launch()V", 7, 1,
            ))
            val job = Any()
            retainedJobs += job
            retainedBlocks += prepared
            graph.asyncTelemetry.coroutineBuilderReturned(prepared, job)
            assertTrue(registry.find(CausalKind.COROUTINE, job) > 0L)
            invokeBlock(prepared)
            assertEquals(0L, registry.find(CausalKind.COROUTINE, job))
            assertEquals(0, registry.liveEntries())
        }
        assertEquals(100, retainedJobs.size)
        assertEquals(100, retainedBlocks.size)
    }

    @Test
    fun exactBuilderBindingSurvivesSuspensionAndThreadMigration() = withSession { graph ->
        lateinit var machine: RootBlock
        var observedId = 0L
        var token = 0L
        val block = RootBlock { root ->
            machine = root
            token = graph.asyncTelemetry.enterCoroutineSegment(machine, "test.StateMachine", true,
                2L, "test.StateMachine#invokeSuspend()V", 12, 1)
            val current = checkNotNull(graph.concurrencyTelemetry.captureCausalContext())
            assertEquals(4, current.entityKind)
            observedId = current.entityId
            graph.asyncTelemetry.exitCoroutineSegment(token, machine, true, CoroutineExecutionOutcome.SUCCESS)
            assertNull(graph.concurrencyTelemetry.captureCausalContext())
            COROUTINE_SUSPENDED
        }
        val wrapped = checkNotNull(graph.asyncTelemetry.wrapCausalCoroutineBlock(block, 1L, "test.Owner#launch()V", 7, 1))
        val job = Any()
        graph.asyncTelemetry.coroutineBuilderReturned(wrapped, job)
        val launchId = checkNotNull(graph.state.collectionEpochs.current?.causalRegistry)
            .find(CausalKind.COROUTINE, job)
        invokeBlock(wrapped)
        assertTrue(token > 0L)
        assertEquals(launchId, observedId)
        val executor = Executors.newSingleThreadExecutor()
        try {
            executor.submit {
                val resumed = graph.asyncTelemetry.enterCoroutineSegment(machine, "test.StateMachine", true,
                    2L, "test.StateMachine#invokeSuspend()V", 12, 1)
                assertEquals(token, resumed)
                assertEquals(observedId, graph.concurrencyTelemetry.captureCausalContext()?.entityId)
                graph.asyncTelemetry.exitCoroutineSegment(resumed, machine, false, CoroutineExecutionOutcome.SUCCESS)
                assertNull(graph.concurrencyTelemetry.captureCausalContext())
            }.get(2L, TimeUnit.SECONDS)
        } finally {
            executor.shutdownNow()
            assertTrue(executor.awaitTermination(2L, TimeUnit.SECONDS))
        }
        assertNull(graph.concurrencyTelemetry.captureCausalContext())
    }

    @Test
    fun sameClassChildWithDifferentCompletionCannotStealBuilderLaunch() = withSession { graph ->
        val registry = checkNotNull(graph.state.collectionEpochs.current?.causalRegistry)
        val child = RootBlock { Unit }
        var childToken = 0L
        var childId = 0L
        var rootId = 0L
        val block = RootBlock { root ->
            childToken = graph.asyncTelemetry.enterCoroutineSegment(child, "test.Child", true,
                2L, "test.Child#invokeSuspend()V", 12, 1)
            childId = checkNotNull(graph.concurrencyTelemetry.captureCausalContext()).entityId
            graph.asyncTelemetry.exitCoroutineSegment(childToken, child, true, CoroutineExecutionOutcome.SUCCESS)
            val rootToken = graph.asyncTelemetry.enterCoroutineSegment(root, "test.Root", true,
                3L, "test.Root#invokeSuspend()V", 13, 1)
            rootId = checkNotNull(graph.concurrencyTelemetry.captureCausalContext()).entityId
            graph.asyncTelemetry.exitCoroutineSegment(rootToken, root, false, CoroutineExecutionOutcome.SUCCESS)
            Unit
        }
        val wrapped = checkNotNull(graph.asyncTelemetry.wrapCausalCoroutineBlock(block,
            1L, "test.Owner#launch()V", 7, 1))
        val job = Any()
        graph.asyncTelemetry.coroutineBuilderReturned(wrapped, job)
        val launchId = registry.find(CausalKind.COROUTINE, job)
        invokeBlock(wrapped)
        assertEquals(launchId, rootId)
        assertTrue(childId > 0L && childId != launchId)
        assertEquals(0L, registry.find(CausalKind.COROUTINE, job))
        val resumed = graph.asyncTelemetry.enterCoroutineSegment(child, "test.Child", true,
            2L, "test.Child#invokeSuspend()V", 12, 1)
        assertEquals(childToken, resumed)
        assertEquals(childId, graph.concurrencyTelemetry.captureCausalContext()?.entityId)
        graph.asyncTelemetry.exitCoroutineSegment(resumed, child, false, CoroutineExecutionOutcome.SUCCESS)
        assertEquals(0, registry.liveEntries())
    }

    @Test
    fun nestedStateMachinesRestoreTheirParentAndCancellationRestoresCaller() = withSession { graph ->
        val parent = Any()
        val child = Any()
        val parentToken = graph.asyncTelemetry.enterCoroutineSegment(parent, "parent", true, 1L, "test.Parent#invokeSuspend()V", 1, 1)
        val parentId = graph.concurrencyTelemetry.captureCausalContext()?.entityId
        val childToken = graph.asyncTelemetry.enterCoroutineSegment(child, "child", true, 2L, "test.Child#invokeSuspend()V", 2, 1)
        assertTrue(parentId != graph.concurrencyTelemetry.captureCausalContext()?.entityId)
        graph.asyncTelemetry.exitCoroutineSegment(childToken, child, false, CoroutineExecutionOutcome.CANCELLED)
        assertEquals(parentId, graph.concurrencyTelemetry.captureCausalContext()?.entityId)
        graph.asyncTelemetry.exitCoroutineSegment(parentToken, parent, false, CoroutineExecutionOutcome.SUCCESS)
        assertNull(graph.concurrencyTelemetry.captureCausalContext())
    }

    private fun invokeBlock(block: Function2<*, *, *>) {
        // Function2 reflection avoids adding another unchecked generic cast to production or tests.
        Function2::class.java.getMethod("invoke", Any::class.java, Any::class.java).invoke(block, Unit,
            object : Continuation<Any?> {
                override val context = EmptyCoroutineContext
                override fun resumeWith(result: Result<Any?>) = result.getOrThrow().let { Unit }
            })
    }

    /** Mirrors SuspendLambda.create: a new same-class instance points to the supplied completion. */
    private class RootBlock(private val body: (RootBlock) -> Any?) : Function2<Any?, Any?, Any?>, CoroutineStackFrame {
        override var callerFrame: CoroutineStackFrame? = null
        override fun getStackTraceElement(): StackTraceElement? = null
        override fun invoke(p1: Any?, p2: Any?): Any? = body(RootBlock(body).also {
            it.callerFrame = p2 as? CoroutineStackFrame
        })
    }

    private inline fun withSession(capacity: Int = 4096, block: (RuntimeComponentGraph) -> Unit) {
        val directory = Files.createTempDirectory("jh-causal-coroutine").toFile()
        val config = JankHunterConfig.builder()
            .availableRuntimeFeatures(setOf(JankHunterRuntimeFeature.CONCURRENCY_DIAGNOSTICS))
            .concurrencyMaxTrackedEntities(capacity)
            .build()
        val writer = AsyncLogWriterFactory().open(directory, config, "causal-coroutine")
        val graph = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1_000L })
        graph.state.bindCurrentConfiguration(config)
        graph.state.runtimeEnabled.set(true)
        graph.state.writer = writer
        graph.coordinator.markStarted(config)
        try { block(graph) } finally {
            graph.coordinator.beginStop()
            writer.close(1_000L)
            val failure = writer.terminalFailureCause()
            directory.deleteRecursively()
            assertNull(failure?.stackTraceToString(), failure)
        }
    }
}
