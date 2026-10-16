package io.jankhunter.runtime

import android.view.View
import java.util.concurrent.Callable

private const val TYPE_CONTRACT_CACHE_CAPACITY = 64
private val runnableTypeContracts = BoundedWeakIdentityCache<Class<*>, TypeContract>(TYPE_CONTRACT_CACHE_CAPACITY)
private val callableTypeContracts = BoundedWeakIdentityCache<Class<*>, TypeContract>(TYPE_CONTRACT_CACHE_CAPACITY)

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
        causal: RuntimeCausalCoroutineTelemetry? = null,
        launch: RuntimeCausalCoroutineLaunch? = null,
    ): Function2<*, *, *>? {
        if (block == null) return block
        if (block is JankHunterCoroutineFunction2) {
            return if (launch == null) block else block.causalPlacement(ownerName, callbacks, causal, launch)
        }
        if (!runtimeActive) return block
        return failOpen(block) {
            @Suppress("UNCHECKED_CAST")
            JankHunterCoroutineFunction2(block as Function2<Any?, Any?, Any?>, ownerName, callbacks, causal, launch)
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
    val cache = if (plainType === Runnable::class.java) runnableTypeContracts else callableTypeContracts
    return cache.getOrPut(valueType) {
        if (computeAdditionalTypeContract(valueType, plainType)) TypeContract.ADDITIONAL else TypeContract.PLAIN
    } == TypeContract.ADDITIONAL
}

private fun computeAdditionalTypeContract(valueType: Class<*>, plainType: Class<*>): Boolean {
    if (valueType.interfaces.any { it != plainType }) return true

    var current: Class<*>? = valueType.superclass
        while (current != null && current != Any::class.java) {
            if (plainType.isAssignableFrom(current)) return true
            current = current.superclass
        }

    return false
}

/** A native queue or factory may rely on the exact submitted application object. */
internal fun preservesNativeExecutorTasks(executor: Any?): Boolean = RuntimeHookGuard.value(true) {
    val pool = executor as? java.util.concurrent.ThreadPoolExecutor ?: return@value true
    // Unknown implementations/subclasses may inspect concrete tasks in execute or callbacks.
    if (pool.javaClass !== java.util.concurrent.ThreadPoolExecutor::class.java &&
        pool.javaClass !== java.util.concurrent.ScheduledThreadPoolExecutor::class.java) return@value true
    pool.queue is java.util.concurrent.PriorityBlockingQueue<*>
}

/** A decorator based on AbstractExecutorService cannot reproduce a host's protected factories. */
internal fun hasCustomExecutorTaskContract(executor: java.util.concurrent.ExecutorService): Boolean {
    return RuntimeHookGuard.value(true) {
        executorTaskContracts.getOrPut(executor.javaClass) {
            if (computeCustomExecutorTaskContract(executor.javaClass)) TypeContract.ADDITIONAL else TypeContract.PLAIN
        } == TypeContract.ADDITIONAL
    }
}

private val executorTaskContracts = BoundedWeakIdentityCache<Class<*>, TypeContract>(TYPE_CONTRACT_CACHE_CAPACITY)
private val standardExecutorClasses = setOf(
    "java.util.concurrent.AbstractExecutorService",
    "java.util.concurrent.ThreadPoolExecutor",
    "java.util.concurrent.ScheduledThreadPoolExecutor",
)
private val nativeTaskContractMethods = setOf(
    "newTaskFor", "decorateTask", "submit", "invokeAll", "invokeAny", "getQueue", "beforeExecute", "afterExecute",
)

private fun computeCustomExecutorTaskContract(type: Class<*>): Boolean {
    var current: Class<*>? = type
    while (current != null && current !== Any::class.java) {
        if (current.name !in standardExecutorClasses &&
            current.declaredMethods.any { it.name in nativeTaskContractMethods }) return true
        current = current.superclass
    }
    return false
}

private enum class TypeContract {
    PLAIN,
    ADDITIONAL,
}
