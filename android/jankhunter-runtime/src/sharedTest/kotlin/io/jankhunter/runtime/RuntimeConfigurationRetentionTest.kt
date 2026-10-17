package io.jankhunter.runtime

import java.lang.ref.ReferenceQueue
import java.lang.ref.WeakReference
import org.junit.Assert.assertNull
import org.junit.Assert.assertEquals
import org.junit.Test

class RuntimeConfigurationRetentionTest {
    @Test
    fun retiredCrashHandlerDoesNotRetainHostCallbacksButKeepsForwarding() {
        val queue = ReferenceQueue<JankHunterRedactor>()
        val pair = retiredHandler(queue)
        for (attempt in 0 until 32) {
            System.gc()
            if (queue.remove(10L) != null) break
        }
        assertNull("A host handler chain kept retired callbacks alive", pair.second.get())
        pair.first.uncaughtException(Thread.currentThread(), IllegalStateException("host"))
    }

    private fun retiredHandler(queue: ReferenceQueue<JankHunterRedactor>): Pair<RuntimeCrashFlushHandler, WeakReference<JankHunterRedactor>> {
        val callback = OldRedactor()
        val reference = WeakReference<JankHunterRedactor>(callback, queue)
        val handler = RuntimeCrashFlushHandler({ _, _ -> }, { callback.redact(it) },
            { true }, { true }, { true }, { true })
        handler.deactivate()
        return handler to reference
    }

    @Test
    fun replacingConfigurationReleasesTheOriginalHostCallback() {
        val state = RuntimeState()
        val queue = ReferenceQueue<JankHunterRedactor>()
        val old = replaceRedactor(state, queue)
        for (attempt in 0 until 32) {
            System.gc()
            if (queue.remove(10L) != null) break
        }
        assertNull("The original configuration must not retain a replaced host callback", old.get())
        assertEquals("test", state.config!!.redactRoute("test"))
    }

    private fun replaceRedactor(state: RuntimeState, queue: ReferenceQueue<JankHunterRedactor>): WeakReference<JankHunterRedactor> {
        val old = OldRedactor()
        val reference = WeakReference<JankHunterRedactor>(old, queue)
        state.bindInitialConfiguration(JankHunterConfig.builder().routeRedactor(old).build())
        state.bindCurrentConfiguration(state.config!!.toBuilder().routeRedactor(JankHunterRedactor.none()).build())
        return reference
    }

    private class OldRedactor : JankHunterRedactor {
        override fun redact(route: String?): String? = "old:$route"
    }
}
