package io.jankhunter.runtime

import io.jankhunter.runtime.internal.system.ObjectRetentionWatcher
import java.util.concurrent.atomic.AtomicBoolean
import org.junit.Assert.assertEquals
import org.junit.Test

class RuntimeRetentionLifecycleSessionTest {
    @Test
    fun legacyCallbackDoesNotRegisterIntoReplacementWatcher() = verifyReplacement(typed = false)

    @Test
    fun typedCallbackDoesNotRegisterIntoReplacementWatcher() = verifyReplacement(typed = true)

    private fun verifyReplacement(typed: Boolean) {
        var now = 0L
        var reports = 0L
        val old = ObjectRetentionWatcher(1000, clock = { now })
        val replacement = ObjectRetentionWatcher(1000, clock = { now }, reporter = { _, _, _, _, count, _, _ -> reports += count })
        enable(old)
        enable(replacement)
        val state = RuntimeState().apply { objectRetentionWatcher = old }
        val access = RuntimeTelemetryAccess(state, ContextTracker(), RuntimeCoordinator(state) { now }, { now }, { 100 })
        val metrics = RuntimeMetricsService(8, { now }, { null }, { null }, {}, { false }, { _, _ -> false })
        val telemetry = RuntimeRetentionTelemetry(state, access, metrics) { now }
        val target = Any()
        val instance = object : JankHunterLifecycleAccessorV1 {
            override fun jankHunterLifecycleKindV1(): Int = 2
            override fun jankHunterVisitLifecycleTargetsV1(sink: JankHunterLifecycleTargetSinkV1, ownerHint: String?) {
                old.stop()
                state.objectRetentionWatcher = replacement
                sink.accept(target, ownerHint)
            }
        }
        try {
            if (typed) telemetry.watchLifecycleObject(instance, 2, "onDestroyView", "owner")
            else telemetry.watchLifecycleObject(instance, "onDestroyView", "owner")
            now = 2000
            replacement.checkRetained()
            assertEquals("Old callback registered into the new runtime", 0L, reports)
            java.lang.ref.Reference.reachabilityFence(target)
        } finally {
            old.stop()
            replacement.stop()
        }
    }

    private fun enable(watcher: ObjectRetentionWatcher) {
        val running = ObjectRetentionWatcher::class.java.getDeclaredField("running").apply { isAccessible = true }
        (running.get(watcher) as AtomicBoolean).set(true)
    }
}
