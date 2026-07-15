package io.jankhunter.runtime

import android.os.Handler
import android.view.View
import java.util.concurrent.Callable

/**
 * Tiny fail-open ABI used by injected bytecode.
 *
 * Keep this facade stateless: loading it must not initialize the much heavier [JankHunter] object.
 * Every entry point owns its complete Throwable boundary and never invokes application business
 * work; wrappers are only prepared or looked up for the original bytecode call site.
 */
internal object JankHunterHooks {
    @JvmStatic
    fun enterMethod(methodId: Long): Long {
        return try {
            JankHunter.enterMethod(methodId)
        } catch (_: Throwable) {
            0L
        }
    }

    @JvmStatic
    fun enterMethod(methodId: Long, methodName: String?): Long {
        return try {
            JankHunter.enterMethod(methodId, methodName)
        } catch (_: Throwable) {
            0L
        }
    }

    @JvmStatic
    fun exitMethod(token: Long, methodId: Long) {
        try {
            JankHunter.exitMethod(token, methodId)
        } catch (_: Throwable) {
        }
    }

    @JvmStatic
    fun recordMethodCall(methodId: Long) {
        try {
            JankHunter.recordMethodCall(methodId)
        } catch (_: Throwable) {
        }
    }

    @JvmStatic
    fun recordMethodCall(methodId: Long, methodName: String?) {
        try {
            JankHunter.recordMethodCall(methodId, methodName)
        } catch (_: Throwable) {
        }
    }

    @JvmStatic
    fun recordCounter(name: String?, value: Long) {
        try {
            JankHunter.recordCounter(name, value)
        } catch (_: Throwable) {
        }
    }

    @JvmStatic
    fun recordLogSpam(ownerName: String?, source: String?, level: Int) {
        try {
            JankHunter.recordLogSpam(ownerName, source, level)
        } catch (_: Throwable) {
        }
    }

    @JvmStatic
    fun wrapRunnable(runnable: Runnable?, ownerName: String?): Runnable? {
        return try {
            JankHunter.wrapRunnable(runnable, ownerName)
        } catch (_: Throwable) {
            runnable
        }
    }

    @JvmStatic
    fun <T> wrapCallable(callable: Callable<T>?, ownerName: String?): Callable<T>? {
        return try {
            JankHunter.wrapCallable(callable, ownerName)
        } catch (_: Throwable) {
            callable
        }
    }

    @JvmStatic
    fun wrapCoroutineBlock(block: Function2<*, *, *>?, ownerName: String?): Function2<*, *, *>? {
        return try {
            JankHunter.wrapCoroutineBlock(block, ownerName)
        } catch (_: Throwable) {
            block
        }
    }

    @JvmStatic
    fun wrapClickListener(listener: View.OnClickListener?, ownerName: String?): View.OnClickListener? {
        return try {
            JankHunter.wrapClickListener(listener, ownerName)
        } catch (_: Throwable) {
            listener
        }
    }

    @JvmStatic
    fun wrapHandlerRunnable(
        handler: Handler?,
        runnable: Runnable?,
        token: Any?,
        ownerName: String?,
    ): Runnable? {
        return try {
            if (handler == null || runnable == null) runnable else JankHunter.wrapHandlerRunnable(
                handler,
                runnable,
                token,
                ownerName,
            )
        } catch (_: Throwable) {
            runnable
        }
    }

    @JvmStatic
    fun onHandlerPostResult(original: Runnable?, wrapped: Runnable?, posted: Boolean) {
        try {
            if (original != null && wrapped != null) {
                JankHunter.onHandlerPostResult(original, wrapped, posted)
            }
        } catch (_: Throwable) {
        }
    }

    @JvmStatic
    fun handlerWrappers(handler: Handler?, runnable: Runnable?, token: Any?): Array<Runnable> {
        return try {
            if (handler == null || runnable == null) emptyArray() else JankHunter.handlerWrappers(
                handler,
                runnable,
                token,
            )
        } catch (_: Throwable) {
            emptyArray()
        }
    }

    @JvmStatic
    fun clearHandlerWrappers(handler: Handler?, runnable: Runnable?, token: Any?) {
        try {
            if (handler != null && runnable != null) {
                JankHunter.clearHandlerWrappers(handler, runnable, token)
            }
        } catch (_: Throwable) {
        }
    }

    @JvmStatic
    fun clearHandlerWrappers(handler: Handler?, token: Any?) {
        try {
            if (handler != null) JankHunter.clearHandlerWrappers(handler, token)
        } catch (_: Throwable) {
        }
    }

    @JvmStatic
    fun enterAnnotatedContext(
        screenName: String?,
        ownerName: String?,
        flowName: String?,
        traceName: String?,
    ): Any? {
        return try {
            JankHunter.enterAnnotatedContext(screenName, ownerName, flowName, traceName)
        } catch (_: Throwable) {
            null
        }
    }

    @JvmStatic
    fun exitAnnotatedContext(token: Any?) {
        try {
            JankHunter.exitAnnotatedContext(token)
        } catch (_: Throwable) {
        }
    }

    @JvmStatic
    fun watchLifecycleObject(instance: Any?, lifecycleEvent: String?, ownerHint: String?) {
        try {
            JankHunter.watchLifecycleObject(instance, lifecycleEvent, ownerHint)
        } catch (_: Throwable) {
        }
    }
}
