package io.jankhunter.runtime.integration

import android.view.Window
import io.jankhunter.runtime.RuntimeHookFailureTracker
import io.jankhunter.runtime.RuntimeHookFailureReason
import java.lang.reflect.InvocationTargetException
import java.lang.reflect.Method
import java.lang.reflect.Proxy
import java.util.concurrent.ConcurrentHashMap

/** Reflection-only bridge into the optional AndroidX JankStats dependency. */
internal object JankHunterJankStats {
    private val frameAccessors = ConcurrentHashMap<Class<*>, FrameAccessors>()
    private val reflectionBridge by lazy(LazyThreadSafetyMode.PUBLICATION, ::loadReflectionBridge)

    fun install(
        window: Window?,
        onFrame: (FrameData) -> Unit,
    ): Handle? {
        if (window == null) return null
        val bridge = reflectionBridge ?: return null

        return try {
            val proxy = Proxy.newProxyInstance(
                bridge.listenerClass.classLoader,
                arrayOf(bridge.listenerClass),
            ) { _, method, args ->
                if (method.name == "onFrame" && args?.isNotEmpty() == true) {
                    try {
                        args[0]?.let(::readFrameData)?.let(onFrame)
                    } catch (throwable: Throwable) {
                        throwable.recordOrRethrow(RuntimeHookFailureReason.JANKSTATS_FRAME)
                    }
                }
                null
            }

            val instance = bridge.createAndTrack.invoke(null, window, proxy)
            if (instance == null) {
                RuntimeHookFailureTracker.record(RuntimeHookFailureReason.JANKSTATS_INSTALL)
                return null
            }
            Handle(instance)
        } catch (throwable: Throwable) {
            throwable.recordOrRethrow(RuntimeHookFailureReason.JANKSTATS_INSTALL)
            null
        }
    }

    internal class Handle(
        private val instance: Any,
    ) {
        @Volatile
        private var installed = true

        @Synchronized
        fun setTrackingEnabled(enabled: Boolean) {
            if (!installed) return
            try {
                instance.javaClass
                    .getMethod("setTrackingEnabled", Boolean::class.javaPrimitiveType)
                    .invoke(instance, enabled)
            } catch (throwable: Throwable) {
                throwable.recordOrRethrow(RuntimeHookFailureReason.JANKSTATS_CONTROL)
            }
        }

        @Synchronized
        fun uninstall() {
            if (!installed) return
            setTrackingEnabled(false)
            installed = false
        }
    }

    internal data class FrameData(
        val isJank: Boolean,
        val durationNanos: Long,
    )

    private fun readFrameData(frameData: Any): FrameData {
        val type = frameData.javaClass
        val accessors = frameAccessors[type] ?: run {
            val created = FrameAccessors(
                isJank = type.methodOrNull("isJank"),
                durationNanos = type.methodOrNull("getFrameDurationUiNanos"),
            )
            frameAccessors.putIfAbsent(type, created) ?: created
        }
        return FrameData(
            isJank = (accessors.isJank.safeInvoke(frameData) as? Boolean) == true,
            durationNanos = (accessors.durationNanos.safeInvoke(frameData) as? Long) ?: 0L,
        )
    }

    private data class FrameAccessors(
        val isJank: Method?,
        val durationNanos: Method?,
    )

    private fun Class<*>.methodOrNull(name: String): Method? {
        return try {
            getMethod(name)
        } catch (throwable: Throwable) {
            throwable.recordOrRethrow(RuntimeHookFailureReason.JANKSTATS_FRAME)
            null
        }
    }

    private fun Method?.safeInvoke(target: Any): Any? {
        return try {
            this?.invoke(target)
        } catch (throwable: Throwable) {
            throwable.recordOrRethrow(RuntimeHookFailureReason.JANKSTATS_FRAME)
            null
        }
    }

    private fun loadReflectionBridge(): ReflectionBridge? {
        return try {
            val jankStatsClass = Class.forName("androidx.metrics.performance.JankStats")
            val listenerClass = Class.forName("androidx.metrics.performance.JankStats\$OnFrameListener")
            ReflectionBridge(
                listenerClass = listenerClass,
                createAndTrack = jankStatsClass.getMethod(
                    "createAndTrack",
                    Window::class.java,
                    listenerClass,
                ),
            )
        } catch (_: ClassNotFoundException) {
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.JANKSTATS_DEPENDENCY_MISSING)
            null
        } catch (_: NoClassDefFoundError) {
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.JANKSTATS_DEPENDENCY_MISSING)
            null
        } catch (throwable: Throwable) {
            throwable.recordOrRethrow(RuntimeHookFailureReason.JANKSTATS_INSTALL)
            null
        }
    }

    private data class ReflectionBridge(
        val listenerClass: Class<*>,
        val createAndTrack: Method,
    )

    private fun Throwable.recordOrRethrow(reason: RuntimeHookFailureReason) {
        val fatal = when (this) {
            is VirtualMachineError,
            is ThreadDeath -> this
            is InvocationTargetException -> targetException?.let { target ->
                when (target) {
                    is VirtualMachineError,
                    is ThreadDeath -> target
                    else -> null
                }
            }
            else -> null
        }
        if (fatal != null) throw fatal
        RuntimeHookFailureTracker.record(reason)
    }
}
