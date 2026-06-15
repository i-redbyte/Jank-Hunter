package io.jankhunter.sample

import android.app.Activity
import android.os.Bundle
import android.os.SystemClock
import android.view.Gravity
import android.view.ViewGroup
import android.widget.Button
import android.widget.LinearLayout
import android.widget.TextView
import io.jankhunter.okhttp3.JankHunterEventListenerFactory
import io.jankhunter.runtime.JankHunter
import okhttp3.OkHttpClient
import okhttp3.Request
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicInteger

class MainActivity : Activity() {
    private val executor = Executors.newSingleThreadExecutor { runnable ->
        Thread(runnable, "JankHunterSampleWorker")
    }
    private val clicks = AtomicInteger()
    private val retainedSamples = mutableListOf<Any>()
    private val networkClient: OkHttpClient by lazy {
        OkHttpClient.Builder()
            .eventListenerFactory(JankHunterEventListenerFactory())
            .build()
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        JankHunter.setScreen("SampleMainActivity")
        setContentView(createContent())
    }

    override fun onDestroy() {
        executor.shutdownNow()
        super.onDestroy()
    }

    private fun createContent(): LinearLayout {
        val density = resources.displayMetrics.density
        val padding = (24 * density).toInt()

        val title = TextView(this).apply {
            text = "Jank Hunter Sample"
            textSize = 24f
        }
        val status = TextView(this).apply {
            text = "Events: 0"
            textSize = 16f
        }
        val networkStatus = TextView(this).apply {
            text = "Network: idle"
            textSize = 14f
        }
        val jankButton = Button(this).apply {
            text = "Record UI Stall"
            setOnClickListener {
                val count = clicks.incrementAndGet()
                JankHunter.withOwner("sample.main.synthetic_stall") {
                    SystemClock.sleep(280)
                }
                JankHunter.recordCounter("sample.ui_stall.clicks", 1)
                status.text = "Events: $count"
            }
        }
        val workerButton = Button(this).apply {
            text = "Record Background Work"
            setOnClickListener {
                executor.execute {
                    val start = SystemClock.elapsedRealtime()
                    SystemClock.sleep(120)
                    JankHunter.recordGauge("sample.worker.duration_ms", SystemClock.elapsedRealtime() - start)
                    JankHunter.flush()
                }
            }
        }
        val watchButton = Button(this).apply {
            text = "Watch Retained Object"
            setOnClickListener {
                val sample = RetainedSample(clicks.get())
                retainedSamples += sample
                JankHunter.watchObject(sample, "io.jankhunter.sample.RetainedSample", "sample.main.retained_button")
                JankHunter.recordCounter("sample.retained.watch.count", 1)
            }
        }
        val jsonPlaceholderButton = Button(this).apply {
            text = "Fetch JSONPlaceholder"
            setOnClickListener {
                runNetworkCall(
                    label = "JSONPlaceholder",
                    owner = "sample.network.jsonplaceholder",
                    url = "https://jsonplaceholder.typicode.com/posts/1",
                    status = networkStatus,
                )
            }
        }
        val httpBinButton = Button(this).apply {
            text = "Fetch HTTP 503"
            setOnClickListener {
                runNetworkCall(
                    label = "httpbin 503",
                    owner = "sample.network.httpbin_503",
                    url = "https://httpbin.org/status/503",
                    status = networkStatus,
                )
            }
        }

        return LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER_HORIZONTAL
            setPadding(padding, padding, padding, padding)
            addView(title, ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
            addView(status, ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
            addView(networkStatus, ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
            addView(jankButton, ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
            addView(workerButton, ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
            addView(watchButton, ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
            addView(jsonPlaceholderButton, ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
            addView(httpBinButton, ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
        }
    }

    private fun runNetworkCall(label: String, owner: String, url: String, status: TextView) {
        status.text = "Network: $label running"

        executor.execute {
            val startedAt = SystemClock.elapsedRealtime()
            var message = "Network: $label failed"

            try {
                var responseCode = 0
                var responseBytes = 0
                JankHunter.withOwner(owner) {
                    val request = Request.Builder().url(url).build()
                    networkClient.newCall(request).execute().use { response ->
                        responseCode = response.code()
                        responseBytes = response.body()?.string()?.length ?: 0
                    }
                }

                if (responseCode >= 400) {
                    JankHunter.recordCounter("sample.network.http_error.count", 1)
                } else {
                    JankHunter.recordCounter("sample.network.success.count", 1)
                }
                JankHunter.recordCounter("sample.network.response_bytes", responseBytes.toLong())
                message = "Network: $label HTTP $responseCode, ${responseBytes}b"
            } catch (throwable: Throwable) {
                JankHunter.recordCounter("sample.network.failure.count", 1)
                message = "Network: $label ${throwable.javaClass.simpleName}"
            } finally {
                JankHunter.recordGauge(
                    "sample.network.duration_ms",
                    SystemClock.elapsedRealtime() - startedAt,
                )
                JankHunter.flush()
                runOnUiThread {
                    status.text = message
                }
            }
        }
    }

    private data class RetainedSample(val id: Int)
}
