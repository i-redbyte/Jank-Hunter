package io.jankhunter.sample.manual

import io.jankhunter.runtime.JankHunterTelemetry

import android.os.SystemClock
import io.jankhunter.okhttp3.JankHunterEventListenerFactory
import io.jankhunter.sample.R
import io.jankhunter.sample.SampleApplication
import io.jankhunter.runtime.JankHunter
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicInteger
import okhttp3.OkHttpClient
import okhttp3.Request

internal class ManualPerformanceScenarios(
    private val application: SampleApplication,
    private val text: SampleText,
    private val stateSink: ManualStateSink,
) {
    private val executor = Executors.newSingleThreadExecutor { runnable ->
        Thread(runnable, "JankHunterManualWorker")
    }
    private val interactions = AtomicInteger()
    private val networkClient by lazy {
        OkHttpClient.Builder()
            .eventListenerFactory(JankHunterEventListenerFactory())
            .build()
    }
    val handlers: Map<ManualAction, ManualActionHandler> = mapOf(
        ManualAction.UI_STALL to { recordUiStall() },
        ManualAction.BACKGROUND_WORK to { recordBackgroundWork() },
        ManualAction.HTTP_SUCCESS to { runHttpSuccess() },
        ManualAction.HTTP_503 to { runHttp503() },
        ManualAction.MEMORY_PRESSURE to { recordMemoryPressure() },
        ManualAction.LOG_SPAM to { recordLogSpamBurst() },
        ManualAction.CUSTOM_METRICS to { recordCustomMetrics() },
    )

    fun recordUiStall() {
        val count = interactions.incrementAndGet()
        JankHunterTelemetry.traceOperation("sample.manual.ui_stall.block_280_ms") {
            JankHunterTelemetry.withOwner("sample.ui.synthetic_stall") {
                SystemClock.sleep(280)
            }
        }
        JankHunterTelemetry.counter("sample.ui_stall.clicks", 1)
        status(text(R.string.status_ui_stall_recorded, count))
    }

    fun recordBackgroundWork() {
        executor.execute {
            val start = SystemClock.elapsedRealtime()
            JankHunterTelemetry.traceOperation("sample.manual.background_work.sleep_140_ms") {
                JankHunterTelemetry.withOwner("sample.worker.expensive_task") {
                    SystemClock.sleep(140)
                }
            }
            JankHunterTelemetry.gauge("sample.worker.duration_ms", SystemClock.elapsedRealtime() - start)
            JankHunterTelemetry.counter("sample.worker.completed.count", 1)
            JankHunter.flush()
            status(text(R.string.status_background_work_recorded))
        }
    }

    fun recordMemoryPressure() {
        application.memoryPressure += ByteArray(384 * 1024)
        val retainedKb = application.memoryPressure.sumOf { it.size.toLong() } / 1024L
        JankHunterTelemetry.gauge("sample.memory.pressure_kb", retainedKb)
        JankHunterTelemetry.counter("sample.memory.pressure.alloc.count", 1)
        status(text(R.string.status_memory_pressure_recorded, retainedKb))
    }

    fun recordLogSpamBurst() {
        repeat(60) {
            JankHunterTelemetry.recordLog("sample.logging.checkout_renderer", "SampleLogger.render", 5)
        }
        JankHunterTelemetry.counter("sample.log_spam.manual_burst.count", 60)
        status(text(R.string.status_log_spam_recorded))
    }

    fun recordCustomMetrics() {
        val count = interactions.incrementAndGet()
        JankHunterTelemetry.counter("sample.checkout.render.count", 1)
        JankHunterTelemetry.gauge("sample.checkout.render_items", 24 + count.toLong())
        JankHunterTelemetry.gauge("sample.checkout.cart_value", 1_990 + count.toLong() * 10)
        status(text(R.string.status_custom_metrics_recorded))
    }

    fun runHttpSuccess() {
        runNetworkCall(
            label = "JSONPlaceholder",
            owner = "sample.network.jsonplaceholder",
            url = "https://jsonplaceholder.typicode.com/posts/1",
        )
    }

    fun runHttp503() {
        runNetworkCall(
            label = "httpbin 503",
            owner = "sample.network.httpbin_503",
            url = "https://httpbin.org/status/503",
        )
    }

    fun close() {
        executor.shutdownNow()
        networkClient.dispatcher().executorService().shutdownNow()
        networkClient.connectionPool().evictAll()
    }

    private fun runNetworkCall(label: String, owner: String, url: String) {
        status(text(R.string.status_network_running, label))
        executor.execute {
            val startedAt = SystemClock.elapsedRealtime()
            var message = text(R.string.status_network_failed, label)
            try {
                var responseCode = 0
                var responseBytes = 0
                JankHunterTelemetry.traceOperation("sample.manual.network.$label") {
                    JankHunterTelemetry.withOwner(owner) {
                        val request = Request.Builder().url(url).build()
                        networkClient.newCall(request).execute().use { response ->
                            responseCode = response.code()
                            responseBytes = response.body()?.bytes()?.size ?: 0
                        }
                    }
                }
                if (responseCode >= 400) {
                    JankHunterTelemetry.counter("sample.network.http_error.count", 1)
                } else {
                    JankHunterTelemetry.counter("sample.network.success.count", 1)
                }
                JankHunterTelemetry.counter("sample.network.response_bytes", responseBytes.toLong())
                message = text(R.string.status_network_http, label, responseCode, responseBytes)
            } catch (throwable: Throwable) {
                JankHunterTelemetry.counter("sample.network.failure.count", 1)
                message = text(R.string.status_network_exception, label, throwable.javaClass.simpleName)
            } finally {
                JankHunterTelemetry.gauge("sample.network.duration_ms", SystemClock.elapsedRealtime() - startedAt)
                JankHunter.flush()
                status(message)
            }
        }
    }

    private fun status(value: String) {
        stateSink.emit(ManualStateUpdate.Status(value))
    }
}
