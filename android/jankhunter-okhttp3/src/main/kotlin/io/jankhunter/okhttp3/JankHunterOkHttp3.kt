package io.jankhunter.okhttp3

import okhttp3.EventListener
import okhttp3.WebSocketListener

object JankHunterOkHttp3 {
    @JvmStatic
    fun wrapEventListenerFactory(factory: EventListener.Factory?): EventListener.Factory? {
        if (factory == null || factory is JankHunterEventListenerFactory) return factory
        return JankHunterEventListenerFactory(factory)
    }

    @JvmStatic
    fun wrapWebSocketListener(listener: WebSocketListener?, ownerName: String?): WebSocketListener? {
        if (listener == null || listener is JankHunterWebSocketListener) return listener
        return JankHunterWebSocketListener(owner = ownerName, delegate = listener)
    }
}
