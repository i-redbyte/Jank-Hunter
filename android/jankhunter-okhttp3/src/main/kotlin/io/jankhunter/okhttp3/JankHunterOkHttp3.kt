package io.jankhunter.okhttp3

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
        val factory = eventListenerFactory(builder)
        builder.eventListenerFactory(wrapEventListenerFactory(factory))
        return builder
    }

    @JvmStatic
    fun wrapWebSocketListener(listener: WebSocketListener?, ownerName: String?): WebSocketListener? {
        if (listener == null || listener is JankHunterWebSocketListener) return listener
        return JankHunterWebSocketListener(owner = ownerName, delegate = listener)
    }

    private fun eventListenerFactory(builder: OkHttpClient.Builder): EventListener.Factory? {
        return runCatching {
            val field = builder.javaClass.getDeclaredField("eventListenerFactory")
            field.isAccessible = true
            field.get(builder) as? EventListener.Factory
        }.getOrNull()
    }
}
