package io.jankhunter.sample.automatic

import io.jankhunter.sample.LocalScenarioServer
import io.jankhunter.sample.graph.NetworkScenarioUseCase
import io.jankhunter.runtime.JankHunter
import java.net.ServerSocket
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.withContext
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener

internal class AutomaticNetworkScenario(
    private val graphScenario: NetworkScenarioUseCase,
) : AutomaticScenarioStage {
    override val step = ScenarioStep.NETWORK
    private var server: LocalScenarioServer? = null

    override suspend fun execute(context: AutomaticStageContext) {
        withContext(Dispatchers.IO) {
            val localServer = LocalScenarioServer().also { server = it }
            try {
                runHttpFlow("sample.auto.network.fast", "fast_response", localServer.port, "/fast")
                runHttpFlow("sample.auto.network.slow", "slow_response", localServer.port, "/slow")
                runHttpFlow("sample.auto.network.server_error", "http_503", localServer.port, "/error")
                runWebSocketFlow(localServer.port)
                val closedPort = ServerSocket(0).use { it.localPort }
                runWebSocketFailureFlow(closedPort)
                runHttpFlow("sample.auto.network.connection_failure", "connection_refused", closedPort, "/offline")
                completeAutomaticStage(ScenarioStep.NETWORK)
            } finally {
                localServer.close()
                server = null
            }
        }
        delay(NEXT_STAGE_DELAY_MS)
    }

    override fun close() {
        server?.close()
        server = null
        graphScenario.close()
    }

    private fun runHttpFlow(flow: String, step: String, port: Int, path: String) {
        JankHunter.withFlow(flow) {
            JankHunter.markFlowStep(step)
            graphScenario.executeHttp(port, path).onSuccess { status ->
                JankHunter.recordCounter("sample.auto.network.http.$status.count", 1)
            }.onFailure {
                JankHunter.recordCounter("sample.auto.network.exception.count", 1)
            }
        }
    }

    private fun runWebSocketFlow(port: Int) {
        JankHunter.withFlow("sample.auto.websocket.success") {
            JankHunter.markFlowStep("open_message_disconnect")
            val completed = CountDownLatch(1)
            val delegate = object : WebSocketListener() {
                override fun onMessage(webSocket: WebSocket, text: String) {
                    JankHunter.recordCounter("sample.auto.websocket.message.count", 1)
                }

                override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
                    webSocket.close(code, reason)
                }

                override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                    JankHunter.recordCounter("sample.auto.websocket.success.count", 1)
                    completed.countDown()
                }

                override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                    completed.countDown()
                }
            }
            graphScenario.openWebSocket(port, "/socket", delegate)
            completed.await(WEBSOCKET_TIMEOUT_SECONDS, TimeUnit.SECONDS)
        }
    }

    private fun runWebSocketFailureFlow(port: Int) {
        JankHunter.withFlow("sample.auto.websocket.failure") {
            JankHunter.markFlowStep("connection_refused")
            val completed = CountDownLatch(1)
            val delegate = object : WebSocketListener() {
                override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                    JankHunter.recordCounter("sample.auto.websocket.expected_failure.count", 1)
                    completed.countDown()
                }
            }
            graphScenario.openWebSocket(port, "/socket-failure", delegate)
            completed.await(WEBSOCKET_TIMEOUT_SECONDS, TimeUnit.SECONDS)
        }
    }

    private companion object {
        const val WEBSOCKET_TIMEOUT_SECONDS = 3L
        const val NEXT_STAGE_DELAY_MS = 500L
    }
}
