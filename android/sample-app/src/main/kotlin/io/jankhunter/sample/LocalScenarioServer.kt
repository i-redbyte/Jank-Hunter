package io.jankhunter.sample

import android.util.Base64
import java.io.BufferedReader
import java.io.Closeable
import java.io.IOException
import java.io.InputStreamReader
import java.net.InetAddress
import java.net.ServerSocket
import java.net.Socket
import java.nio.charset.StandardCharsets
import java.security.MessageDigest
import java.util.Locale
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean

internal class LocalScenarioServer : Closeable {
    private val running = AtomicBoolean(true)
    private val serverSocket = ServerSocket(0, 16, InetAddress.getByName(LOOPBACK))
    private val workers: ExecutorService = Executors.newCachedThreadPool { runnable ->
        Thread(runnable, "SampleLoopbackServer")
    }

    val port: Int = serverSocket.localPort

    init {
        workers.execute {
            while (running.get()) {
                val socket = runCatching { serverSocket.accept() }.getOrNull() ?: break
                workers.execute { handleSafely(socket) }
            }
        }
    }

    override fun close() {
        running.set(false)
        runCatching { serverSocket.close() }
        workers.shutdownNow()
    }

    private fun handleSafely(socket: Socket) {
        try {
            handle(socket)
        } catch (_: IOException) {
            Unit
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
        }
    }

    private fun handle(socket: Socket) {
        socket.use { connection ->
            connection.soTimeout = SOCKET_TIMEOUT_MS
            val reader = BufferedReader(InputStreamReader(connection.getInputStream(), StandardCharsets.US_ASCII))
            val requestLine = reader.readLine() ?: return
            val path = requestLine.split(' ').getOrNull(1) ?: "/"
            val headers = linkedMapOf<String, String>()
            while (true) {
                val line = reader.readLine() ?: return
                if (line.isEmpty()) break
                val separator = line.indexOf(':')
                if (separator > 0) {
                    headers[line.substring(0, separator).lowercase(Locale.US)] = line.substring(separator + 1).trim()
                }
            }
            if (headers["upgrade"]?.equals("websocket", ignoreCase = true) == true) {
                handleWebSocket(connection, headers["sec-websocket-key"] ?: return)
                return
            }
            when (path) {
                "/fast" -> writeHttp(connection, 200, "fast-response")
                "/slow" -> {
                    Thread.sleep(SLOW_RESPONSE_MS)
                    writeHttp(connection, 200, "slow-response")
                }
                "/error" -> writeHttp(connection, 503, "service-unavailable")
                else -> writeHttp(connection, 404, "not-found")
            }
        }
    }

    private fun writeHttp(socket: Socket, status: Int, body: String) {
        val reason = when (status) {
            200 -> "OK"
            503 -> "Service Unavailable"
            else -> "Not Found"
        }
        val bodyBytes = body.toByteArray(StandardCharsets.UTF_8)
        val head = buildString {
            append("HTTP/1.1 $status $reason\r\n")
            append("Content-Type: text/plain\r\n")
            append("Content-Length: ${bodyBytes.size}\r\n")
            append("Connection: close\r\n\r\n")
        }.toByteArray(StandardCharsets.US_ASCII)
        socket.getOutputStream().apply {
            write(head)
            write(bodyBytes)
            flush()
        }
    }

    private fun handleWebSocket(socket: Socket, key: String) {
        val digest = MessageDigest.getInstance("SHA-1")
            .digest((key + WEBSOCKET_GUID).toByteArray(StandardCharsets.US_ASCII))
        val accept = Base64.encodeToString(digest, Base64.NO_WRAP)
        val response = buildString {
            append("HTTP/1.1 101 Switching Protocols\r\n")
            append("Upgrade: websocket\r\n")
            append("Connection: Upgrade\r\n")
            append("Sec-WebSocket-Accept: $accept\r\n\r\n")
        }.toByteArray(StandardCharsets.US_ASCII)
        val message = "loopback-message".toByteArray(StandardCharsets.UTF_8)
        socket.getOutputStream().apply {
            write(response)
            write(byteArrayOf(0x81.toByte(), message.size.toByte()))
            write(message)
            flush()
            Thread.sleep(WEBSOCKET_CLOSE_DELAY_MS)
            write(byteArrayOf(0x88.toByte(), 0x02, 0x03, 0xE8.toByte()))
            flush()
        }
    }

    private companion object {
        const val LOOPBACK = "127.0.0.1"
        const val SOCKET_TIMEOUT_MS = 5_000
        const val SLOW_RESPONSE_MS = 700L
        const val WEBSOCKET_CLOSE_DELAY_MS = 50L
        const val WEBSOCKET_GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
    }
}
