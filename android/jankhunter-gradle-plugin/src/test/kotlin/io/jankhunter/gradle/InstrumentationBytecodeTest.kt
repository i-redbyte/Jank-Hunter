package io.jankhunter.gradle

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class InstrumentationBytecodeTest {
    @Test
    fun commandMetadataDistinguishesReplacementHooks() {
        assertTrue(
            BytecodeCommandFactory.commandFor(
                HookIntent.HandlerRunnable(HandlerRunnableKind.SINGLE_RUNNABLE),
            ).replacesOriginalCall,
        )
        assertTrue(
            BytecodeCommandFactory.commandFor(
                HookIntent.HandlerRemoveCallbacks(HandlerRemoveCallbacksKind.RUNNABLE),
            ).replacesOriginalCall,
        )
        assertTrue(BytecodeCommandFactory.commandFor(HookIntent.HandlerHasCallbacks).replacesOriginalCall)

        assertFalse(BytecodeCommandFactory.commandFor(HookIntent.WrapOkHttpEventListenerFactory).replacesOriginalCall)
        assertFalse(
            BytecodeCommandFactory.commandFor(
                HookIntent.ExecutorRunnable(ExecutorRunnableKind.SINGLE_RUNNABLE),
            ).replacesOriginalCall,
        )
        assertFalse(BytecodeCommandFactory.commandFor(HookIntent.LogSpam("android.util.Log.d", 3)).replacesOriginalCall)
    }
}

