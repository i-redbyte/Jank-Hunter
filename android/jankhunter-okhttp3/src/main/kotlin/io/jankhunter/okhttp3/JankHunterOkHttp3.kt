package io.jankhunter.okhttp3

import io.jankhunter.runtime.JankHunter
import okhttp3.EventListener
import okhttp3.OkHttpClient
import okhttp3.WebSocketListener

object JankHunterOkHttp3 {
    @JvmStatic
    fun wrapEventListenerFactory(factory: EventListener.Factory?): EventListener.Factory {
        if (factory is JankHunterEventListenerFactory) return factory
        return JankHunterEventListenerFactory(factory)
    }

    @JvmStatic
    fun installEventListenerFactory(builder: OkHttpClient.Builder): OkHttpClient.Builder {
        when (val lookup = eventListenerFactory(builder)) {
            is EventListenerFactoryLookup.Found -> {
                builder.eventListenerFactory(wrapEventListenerFactory(lookup.factory))
            }
            EventListenerFactoryLookup.Unavailable -> {
                JankHunter.recordCounter("jankhunter.okhttp.event_listener_factory.lookup_failed.count", 1)
            }
        }
        return builder
    }

    @JvmStatic
    fun wrapWebSocketListener(listener: WebSocketListener?, ownerName: String?): WebSocketListener? {
        if (listener == null || listener is JankHunterWebSocketListener) return listener
        return JankHunterWebSocketListener(owner = ownerName, delegate = listener)
    }

    private fun eventListenerFactory(builder: OkHttpClient.Builder): EventListenerFactoryLookup {
        return runCatching {
            val field = builder.javaClass.getDeclaredField("eventListenerFactory")
            field.isAccessible = true
            EventListenerFactoryLookup.Found(field.get(builder) as? EventListener.Factory)
        }.getOrElse {
            EventListenerFactoryLookup.Unavailable
        }
    }

    private sealed class EventListenerFactoryLookup {
        data class Found(val factory: EventListener.Factory?) : EventListenerFactoryLookup()
        data object Unavailable : EventListenerFactoryLookup()
    }
}
