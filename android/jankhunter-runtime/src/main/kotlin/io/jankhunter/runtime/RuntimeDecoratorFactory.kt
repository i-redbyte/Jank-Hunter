package io.jankhunter.runtime

import android.view.View
import java.util.concurrent.Callable

internal fun wrapRunnableDecorator(
        runnable: Runnable?,
        ownerName: String?,
        runtimeActive: Boolean,
        callbacks: RuntimeAsyncCallbacks,
    ): Runnable? {
        if (runnable == null || runnable is JankHunterRunnable) return runnable
        if (!runtimeActive) return runnable
        if (hasAdditionalTypeContract(runnable, Runnable::class.java)) return runnable
        return failOpen(runnable) {
            JankHunterRunnable(runnable, ownerName, callbacks)
        }
    }

internal fun wrapHandlerRunnableDecorator(
        runnable: Runnable,
        ownerName: String?,
        runtimeActive: Boolean,
        callbacks: RuntimeAsyncCallbacks,
        owner: HandlerRunnableOwner,
    ): Runnable {
        if (runnable is JankHunterHandlerRunnable || runnable is JankHunterRunnable) return runnable
        if (!runtimeActive) return runnable
        return failOpen(runnable) {
            JankHunterHandlerRunnable(runnable, ownerName, callbacks, owner)
        }
    }

internal fun <T> wrapCallableDecorator(
        callable: Callable<T>?,
        ownerName: String?,
        runtimeActive: Boolean,
        callbacks: RuntimeAsyncCallbacks,
    ): Callable<T>? {
        if (callable == null || callable is JankHunterCallable<*>) return callable
        if (!runtimeActive) return callable
        if (hasAdditionalTypeContract(callable, Callable::class.java)) return callable
        return failOpen(callable) {
            JankHunterCallable(callable, ownerName, callbacks)
        }
    }

internal fun wrapCoroutineBlockDecorator(
        block: Function2<*, *, *>?,
        ownerName: String?,
        runtimeActive: Boolean,
        callbacks: RuntimeAsyncCallbacks,
    ): Function2<*, *, *>? {
        if (block == null || block is JankHunterCoroutineFunction2) return block
        if (!runtimeActive) return block
        return failOpen(block) {
            @Suppress("UNCHECKED_CAST")
            JankHunterCoroutineFunction2(block as Function2<Any?, Any?, Any?>, ownerName, callbacks)
        }
    }

internal fun wrapClickListenerDecorator(
        listener: View.OnClickListener?,
        ownerName: String?,
        runtimeActive: Boolean,
        callbacks: RuntimeAsyncCallbacks,
    ): View.OnClickListener? {
        if (listener == null || listener is JankHunterClickListener) return listener
        if (!runtimeActive) return listener
        return failOpen(listener) {
            JankHunterClickListener(listener, ownerName, callbacks)
        }
    }

private inline fun <T> failOpen(original: T, create: () -> T): T {
        return RuntimeHookGuard.value(original, create)
    }

private fun hasAdditionalTypeContract(value: Any, plainType: Class<*>): Boolean {
        val valueType = value.javaClass
        if (valueType.interfaces.any { it != plainType }) return true

        var current = valueType.superclass
        while (current != null && current != Any::class.java) {
            if (plainType.isAssignableFrom(current)) return true
            current = current.superclass
        }

        return false
}
