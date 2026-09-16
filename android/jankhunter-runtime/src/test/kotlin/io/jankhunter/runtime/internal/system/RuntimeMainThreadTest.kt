package io.jankhunter.runtime.internal.system

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeMainThreadTest {
    @Test
    fun mainThreadUsesIdentityInsteadOfThreadName() {
        val current = Thread.currentThread()
        val renamedMain = Thread("worker-looking-main")
        val backgroundNamedMain = Thread("main")

        assertTrue(isRuntimeMainThread(currentThread = renamedMain, mainThread = renamedMain))
        assertFalse(isRuntimeMainThread(currentThread = backgroundNamedMain, mainThread = renamedMain))
        assertFalse(isRuntimeMainThread(currentThread = current, mainThread = null))
    }
}
