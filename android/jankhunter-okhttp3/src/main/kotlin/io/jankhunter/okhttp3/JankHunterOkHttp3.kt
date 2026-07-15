package io.jankhunter.okhttp3

import io.jankhunter.runtime.JankHunter
import okhttp3.EventListener
import okhttp3.OkHttpClient
import okhttp3.WebSocketListener

/** Stateless fail-open ABI used by the Gradle instrumentation. */
object JankHunterOkHttp3 {
    @JvmStatic
    fun wrapEventListenerFactory(factory: EventListener.Factory?): EventListener.Factory? {
        return okHttpNonFatalOr(factory) {
            if (factory is JankHunterEventListenerFactory) factory else JankHunterEventListenerFactory(factory)
        }
    }

    @JvmStatic
    fun installEventListenerFactory(builder: OkHttpClient.Builder?): OkHttpClient.Builder? {
        if (builder == null) return null
        return okHttpNonFatalOr(builder) {
            when (val lookup = eventListenerFactory(builder)) {
                is EventListenerFactoryLookup.Found -> {
                    val wrapped = wrapEventListenerFactory(lookup.factory)
                    if (wrapped != null) builder.eventListenerFactory(wrapped)
                }
                EventListenerFactoryLookup.Unavailable -> recordLookupFailure(null)
                is EventListenerFactoryLookup.Unsupported -> recordLookupFailure(lookup.reason)
            }
            builder
        }
    }

    @JvmStatic
    fun wrapWebSocketListener(listener: WebSocketListener?, ownerName: String?): WebSocketListener? {
        return okHttpNonFatalOr(listener) {
            if (listener == null || listener is JankHunterWebSocketListener) {
                listener
            } else {
                JankHunterWebSocketListener(owner = ownerName, delegate = listener)
            }
        }
    }

    private fun recordLookupFailure(reason: String?) {
        okHttpNonFatal {
            JankHunter.recordCounter("jankhunter.okhttp.event_listener_factory.lookup_failed.count", 1)
            if (reason != null) {
                JankHunter.recordCounter("jankhunter.okhttp.event_listener_factory.$reason.count", 1)
            }
        }
    }

    private fun eventListenerFactory(builder: OkHttpClient.Builder): EventListenerFactoryLookup {
        return okHttpNonFatalOr(EventListenerFactoryLookup.Unavailable) {
            val field = generateSequence<Class<*>>(builder.javaClass) { it.superclass }
                .flatMap { it.declaredFields.asSequence() }
                .firstOrNull { candidate ->
                    candidate.name == "eventListenerFactory" &&
                        EventListener.Factory::class.java.isAssignableFrom(candidate.type)
                }
                ?: return@okHttpNonFatalOr EventListenerFactoryLookup.Unsupported("unsupported_builder_layout")
            field.isAccessible = true
            val factory = field.get(builder)
                ?: return@okHttpNonFatalOr EventListenerFactoryLookup.Found(null)
            EventListenerFactoryLookup.Found(factory as EventListener.Factory)
        }
    }

    private sealed class EventListenerFactoryLookup {
        data class Found(val factory: EventListener.Factory?) : EventListenerFactoryLookup()
        data object Unavailable : EventListenerFactoryLookup()
        data class Unsupported(val reason: String) : EventListenerFactoryLookup()
    }

    private inline fun okHttpNonFatal(block: () -> Unit) {
        try {
            block()
        } catch (throwable: Throwable) {
            throwable.rethrowOkHttpFatal()
        }
    }

    private inline fun <T> okHttpNonFatalOr(fallback: T, block: () -> T): T {
        return try {
            block()
        } catch (throwable: Throwable) {
            throwable.rethrowOkHttpFatal()
            fallback
        }
    }

    private fun Throwable.rethrowOkHttpFatal() {
        if (this is VirtualMachineError || this is ThreadDeath) throw this
    }
}
