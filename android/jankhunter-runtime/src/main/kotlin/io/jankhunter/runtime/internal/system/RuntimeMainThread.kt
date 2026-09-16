package io.jankhunter.runtime.internal.system

import android.os.Handler
import android.os.Looper

internal fun isRuntimeMainThread(
    currentThread: Thread = Thread.currentThread(),
    mainThread: Thread? = runtimeMainThread(),
): Boolean = mainThread != null && currentThread === mainThread

internal class RuntimeMainThreadDispatcher(
    private val isMainThread: () -> Boolean = ::isRuntimeMainThread,
    private val postToMain: (() -> Unit) -> Boolean = { task ->
        val mainLooper = Looper.getMainLooper()
        if (mainLooper == null) false else Handler(mainLooper).post(task)
    },
) {
    fun dispatch(task: () -> Unit): Boolean {
        if (isMainThread()) {
            task()
            return true
        }
        return try {
            postToMain(task)
        } catch (error: Throwable) {
            if (error is VirtualMachineError || error is ThreadDeath) throw error
            false
        }
    }
}

private fun runtimeMainThread(): Thread? {
    return try {
        Looper.getMainLooper()?.thread
    } catch (error: Throwable) {
        if (error is VirtualMachineError || error is ThreadDeath) throw error
        null
    }
}
