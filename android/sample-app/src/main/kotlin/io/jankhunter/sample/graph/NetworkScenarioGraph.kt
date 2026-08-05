package io.jankhunter.sample.graph

import io.jankhunter.runtime.JankHunter
import java.io.Closeable
import java.net.Proxy
import java.util.concurrent.TimeUnit
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.WebSocket
import okhttp3.WebSocketListener

internal class NetworkScenarioUseCase(
    private val repository: CheckoutNetworkRepository,
) : Closeable {
    fun executeHttp(port: Int, path: String): Result<Int> {
        var result: Result<Int>? = null
        JankHunter.withOwner(CheckoutApi::class.java.name) {
            result = runCatching { repository.executeHttp(port, path) }
        }
        return requireNotNull(result)
    }

    fun openWebSocket(port: Int, path: String, listener: WebSocketListener): WebSocket {
        var webSocket: WebSocket? = null
        JankHunter.withOwner(CheckoutApi::class.java.name) {
            webSocket = repository.openWebSocket(port, path, listener)
        }
        return requireNotNull(webSocket)
    }

    override fun close() {
        repository.close()
    }
}

internal class CheckoutNetworkRepository(
    private val api: CheckoutApi,
) : Closeable {
    fun executeHttp(port: Int, path: String): Int {
        return api.execute("http://127.0.0.1:$port$path")
    }

    fun openWebSocket(port: Int, path: String, listener: WebSocketListener): WebSocket {
        return api.openWebSocket("ws://127.0.0.1:$port$path", listener)
    }

    override fun close() {
        api.close()
    }
}

internal class CheckoutApi : Closeable {
    private val client = OkHttpClient.Builder()
        .proxy(Proxy.NO_PROXY)
        .connectTimeout(NETWORK_TIMEOUT_SECONDS, TimeUnit.SECONDS)
        .readTimeout(NETWORK_TIMEOUT_SECONDS, TimeUnit.SECONDS)
        .build()

    fun execute(url: String): Int {
        val request = Request.Builder().url(url).build()
        return client.newCall(request).execute().use { response ->
            response.body()?.bytes()
            response.code()
        }
    }

    fun openWebSocket(url: String, listener: WebSocketListener): WebSocket {
        val request = Request.Builder().url(url).build()
        return client.newWebSocket(request, listener)
    }

    override fun close() {
        client.dispatcher().executorService().shutdownNow()
        client.connectionPool().evictAll()
    }

    private companion object {
        const val NETWORK_TIMEOUT_SECONDS = 3L
    }
}
