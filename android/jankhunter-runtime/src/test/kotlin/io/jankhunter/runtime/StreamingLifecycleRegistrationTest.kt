package io.jankhunter.runtime

import io.jankhunter.runtime.internal.system.ObjectRetentionWatcher
import java.util.concurrent.atomic.AtomicBoolean
import org.junit.Assert.assertEquals
import org.junit.Test

class StreamingLifecycleRegistrationTest {
    @Test fun registersBeforeAccessorReturnsAndBoundsTheRegistry() {
        val watcher = watcher(2)
        val state = RuntimeState().apply { objectRetentionWatcher = watcher }
        val targets = List(1000) { Any() }
        val accessor = accessor { sink ->
            targets.forEachIndexed { index, target ->
                sink.accept(target, "owner")
                assertEquals(minOf(index + 1, 2), size(watcher))
            }
            sink.accept(targets.first(), "alias")
            assertEquals(2, size(watcher))
        }
        try {
            telemetry(state).watchLifecycleObject(accessor, 2, "onDestroyView", "owner")
            assertEquals(2, size(watcher))
        } finally { watcher.stop() }
    }

    @Test fun emittedTargetsStayWithOriginalWatcherAfterReconfigure() {
        val old = watcher(8)
        val replacement = watcher(8)
        val state = RuntimeState().apply { objectRetentionWatcher = old }
        val first = Any()
        val second = Any()
        val accessor = accessor { sink ->
            sink.accept(first, "owner")
            assertEquals(1, size(old))
            state.objectRetentionWatcher = replacement
            state.lifecycleGeneration++
            sink.accept(second, "owner")
        }
        try {
            telemetry(state).watchLifecycleObject(accessor, "onDestroyView", "owner")
            assertEquals(1, size(old))
            assertEquals(0, size(replacement))
        } finally { old.stop(); replacement.stop() }
    }

    @Test fun changedGenerationRejectsLaterTargetsEvenWithSameWatcher() {
        val watcher = watcher(8)
        val state = RuntimeState().apply { objectRetentionWatcher = watcher }
        val first = Any()
        val second = Any()
        val accessor = accessor { sink ->
            sink.accept(first, "owner")
            state.lifecycleGeneration++
            sink.accept(second, "owner")
        }
        try {
            telemetry(state).watchLifecycleObject(accessor, 2, "onDestroyView", "owner")
            assertEquals(1, size(watcher))
        } finally { watcher.stop() }
    }

    private fun accessor(emit: (JankHunterLifecycleTargetSinkV1) -> Unit) = object : JankHunterLifecycleAccessorV1 {
        override fun jankHunterLifecycleKindV1() = 2
        override fun jankHunterVisitLifecycleTargetsV1(sink: JankHunterLifecycleTargetSinkV1, ownerHint: String?) = emit(sink)
    }
    private fun watcher(capacity: Int) = ObjectRetentionWatcher(1000, maxWatchedReferences = capacity).also {
        val field = ObjectRetentionWatcher::class.java.getDeclaredField("running").apply { isAccessible = true }
        (field.get(it) as AtomicBoolean).set(true)
    }
    private fun size(watcher: ObjectRetentionWatcher): Int {
        val field = ObjectRetentionWatcher::class.java.getDeclaredField("watched").apply { isAccessible = true }
        return (field.get(watcher) as List<*>).size
    }
    private fun telemetry(state: RuntimeState): RuntimeRetentionTelemetry {
        val access = RuntimeTelemetryAccess(state, ContextTracker(), RuntimeCoordinator(state) { 0L }, { 0L }, { 100 })
        val metrics = RuntimeMetricsService(8, { 0L }, { null }, { null }, {}, { false }, { _, _ -> false })
        return RuntimeRetentionTelemetry(state, access, metrics) { 0L }
    }
}
