package io.jankhunter.sample

import android.app.Activity
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
import android.os.Bundle
import android.os.SystemClock
import android.view.View
import android.view.ViewGroup
import android.widget.Button
import android.widget.LinearLayout
import android.widget.ScrollView
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
    private val memoryPressure = mutableListOf<ByteArray>()
    private val networkClient: OkHttpClient by lazy {
        OkHttpClient.Builder()
            .eventListenerFactory(JankHunterEventListenerFactory())
            .build()
    }

    private lateinit var statusText: TextView
    private lateinit var runtimeFlagText: TextView
    private lateinit var leakCanaryStatusText: TextView

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        LeakCanaryBridge.configure()
        JankHunter.setScreen("SamplePlayground")
        setContentView(createContent())
        refreshRuntimeFlag("ready")
        refreshLeakCanaryStatus("ready")
    }

    override fun onDestroy() {
        executor.shutdownNow()
        super.onDestroy()
    }

    private fun createContent(): ScrollView {
        val content = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(18), dp(18), dp(18), dp(24))
            setBackgroundColor(BG)
            addView(hero())
            addView(controlTower())
            addView(featureFlagLab())
            addView(performanceLab())
            addView(leakLab())
            addView(leakCanaryBenchmarkLab())
            addView(compareLab())
        }
        return ScrollView(this).apply {
            setBackgroundColor(BG)
            addView(content, ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
        }
    }

    private fun hero(): View {
        statusText = TextView(this).apply {
            text = "Ready: run a baseline, create a regression, flush, then pull the report."
            textSize = 14f
            setTextColor(MUTED)
        }
        return card {
            addView(label("Jank Hunter · sample lab", CYAN))
            addView(title("Performance & Leak Playground"))
            addView(copy("A guided demo for UI stalls, network, memory pressure, log spam, custom metrics, retained objects, LeakCanary comparison, compare reports, and dynamic SDK feature flags."))
            addView(statusText)
        }
    }

    private fun controlTower(): View = section(
        title = "Control tower",
        subtitle = "One-tap scenarios for clean baseline and noisy candidate runs.",
        actions = listOf(
            action("Run clean baseline", OK) { runCleanBaseline() },
            action("Run noisy candidate", MAGENTA) { runNoisyCandidate() },
            action("Flush diagnostics", CYAN) {
                JankHunter.flush()
                updateStatus("Flushed. Pull .jhlog with run-sample-app.sh or collect-android-leak-report.sh.")
            },
        ),
    )

    private fun featureFlagLab(): View {
        runtimeFlagText = TextView(this).apply {
            textSize = 14f
            setTextColor(MUTED)
        }
        return section(
            title = "Feature flag",
            subtitle = "Dynamic runtime switch for Remote Config, kill switches, staged rollout and QA toggles.",
            beforeActions = listOf(runtimeFlagText),
            actions = listOf(
                action("Enable SDK runtime", OK) {
                    val enabled = JankHunter.setRuntimeEnabled(true, "sample_feature_flag")
                    JankHunter.setScreen("SamplePlayground")
                    refreshRuntimeFlag(if (enabled) "enabled from feature flag" else "enable failed")
                },
                action("Disable SDK runtime", WARN) {
                    JankHunter.setRuntimeEnabled(false, "sample_feature_flag")
                    refreshRuntimeFlag("disabled from feature flag")
                },
                action("Record flag probe", CYAN) {
                    JankHunter.recordCounter("sample.feature_flag.probe.count", 1)
                    JankHunter.recordGauge("sample.feature_flag.runtime_enabled", if (JankHunter.isRuntimeEnabled()) 1 else 0)
                    refreshRuntimeFlag("probe recorded")
                },
            ),
        )
    }

    private fun performanceLab(): View = section(
        title = "Performance lab",
        subtitle = "Signals that show up in inspect, math, influence and code-problem reports.",
        actions = listOf(
            action("UI stall", WARN) { recordUiStall() },
            action("Background work", CYAN) { recordBackgroundWork() },
            action("HTTP success", OK) {
                runNetworkCall(
                    label = "JSONPlaceholder",
                    owner = "sample.network.jsonplaceholder",
                    url = "https://jsonplaceholder.typicode.com/posts/1",
                )
            },
            action("HTTP 503", BAD) {
                runNetworkCall(
                    label = "httpbin 503",
                    owner = "sample.network.httpbin_503",
                    url = "https://httpbin.org/status/503",
                )
            },
            action("Memory pressure", MAGENTA) { recordMemoryPressure() },
            action("Log spam", BAD) { recordLogSpamBurst() },
            action("Custom metrics", CYAN) { recordCustomMetrics() },
        ),
    )

    private fun leakLab(): View = section(
        title = "Leak lab",
        subtitle = "The same retained objects are watched by Jank Hunter and, in debug builds, LeakCanary.",
        actions = listOf(
            action("Clean object", OK) { recordCleanObject() },
            action("Activity reference", MAGENTA) {
                recordLeakDemo(
                    step = "activity_reference",
                    owner = "sample.memory_leak.activity_registry",
                    className = "io.jankhunter.sample.LeakedCheckoutActivity",
                ) {
                    LeakedActivityScreen(clicks.incrementAndGet())
                }
            },
            action("View binding", MAGENTA) {
                recordLeakDemo(
                    step = "view_binding",
                    owner = "sample.memory_leak.binding_cache",
                    className = "io.jankhunter.sample.LeakedCheckoutBinding",
                ) {
                    LeakedBindingSnapshot(screen = this@MainActivity, payload = ByteArray(96 * 1024))
                }
            },
            action("Listener callback", WARN) {
                recordLeakDemo(
                    step = "listener_callback",
                    owner = "sample.memory_leak.listener_registry",
                    className = "io.jankhunter.sample.LeakedPaymentListener",
                ) {
                    LeakedPaymentListener {
                        updateStatus("Listener callback is still retained.")
                    }
                }
            },
            action("Cache entries", WARN) { recordCacheLeaks() },
            action("Clear retained list", OK) {
                retainedSamples.clear()
                JankHunter.recordCounter("sample.memory_leak.retained_list.clear.count", 1)
                updateStatus("Retained sample list cleared.")
            },
        ),
    )

    private fun leakCanaryBenchmarkLab(): View {
        leakCanaryStatusText = TextView(this).apply {
            textSize = 14f
            setTextColor(MUTED)
        }
        return section(
            title = "LeakCanary benchmark",
            subtitle = "Run matched scenarios, then compare Jank Hunter HTML with LeakCanary heap analysis.",
            beforeActions = listOf(leakCanaryStatusText),
            actions = listOf(
                action("Both: clean object", OK) {
                    recordCleanObject()
                    refreshLeakCanaryStatus("clean object queued")
                },
                action("Both: retained object", MAGENTA) {
                    recordLeakDemo(
                        step = "leakcanary_retained_object",
                        owner = "sample.memory_leak.leakcanary_benchmark",
                        className = "io.jankhunter.sample.LeakedCheckoutActivity",
                    ) {
                        LeakedActivityScreen(clicks.incrementAndGet())
                    }
                    refreshLeakCanaryStatus("retained object queued")
                },
                action("Both: cache burst", BAD) {
                    recordCacheLeaks()
                    refreshLeakCanaryStatus("cache burst queued")
                },
                action("How to compare", CYAN) {
                    JankHunter.flush()
                    updateStatus("Jank Hunter: pull report-leaks.html. LeakCanary: wait a few seconds, background the app, then open notification or launcher report.")
                    refreshLeakCanaryStatus("comparison hint shown")
                },
            ),
        )
    }

    private fun compareLab(): View = section(
        title = "Compare lab",
        subtitle = "Create candidate-only regressions, then compare baseline and candidate logs.",
        actions = listOf(
            action("Candidate leak burst", BAD) { recordLeakRegressionBurst() },
            action("Candidate perf burst", BAD) {
                repeat(2) { recordUiStall() }
                recordMemoryPressure()
                recordLogSpamBurst()
                updateStatus("Candidate performance burst recorded.")
            },
            action("Pull report hint", CYAN) {
                JankHunter.flush()
                updateStatus("Use: run-sample-app.sh -> log/report, or cli/scripts/collect-android-leak-report.sh.")
            },
        ),
    )

    private fun runCleanBaseline() {
        retainedSamples.clear()
        memoryPressure.clear()
        JankHunter.withFlow("sample.guided.baseline") {
            JankHunter.markFlowStep("clean_probe")
            recordCustomMetrics()
            recordCleanObject()
        }
        JankHunter.flush()
        updateStatus("Baseline recorded: clean object, custom metrics, no retained sample list.")
    }

    private fun runNoisyCandidate() {
        JankHunter.withFlow("sample.guided.candidate") {
            JankHunter.markFlowStep("regression_pack")
            recordUiStall()
            recordMemoryPressure()
            recordCacheLeaks()
            recordLeakRegressionBurst()
        }
        JankHunter.flush()
        updateStatus("Candidate recorded: UI, memory and leak regressions are ready for compare.")
    }

    private fun recordUiStall() {
        val count = clicks.incrementAndGet()
        JankHunter.withOwner("sample.ui.synthetic_stall") {
            SystemClock.sleep(280)
        }
        JankHunter.recordCounter("sample.ui_stall.clicks", 1)
        updateStatus("UI stall recorded (#$count).")
    }

    private fun recordBackgroundWork() {
        executor.execute {
            val start = SystemClock.elapsedRealtime()
            JankHunter.withOwner("sample.worker.expensive_task") {
                SystemClock.sleep(140)
            }
            JankHunter.recordGauge("sample.worker.duration_ms", SystemClock.elapsedRealtime() - start)
            JankHunter.recordCounter("sample.worker.completed.count", 1)
            JankHunter.flush()
            runOnUiThread { updateStatus("Background work recorded.") }
        }
    }

    private fun recordMemoryPressure() {
        val chunk = ByteArray(384 * 1024)
        memoryPressure += chunk
        val retainedKb = memoryPressure.sumOf { it.size / 1024L }
        JankHunter.recordGauge("sample.memory.pressure_kb", retainedKb)
        JankHunter.recordCounter("sample.memory.pressure.alloc.count", 1)
        updateStatus("Memory pressure recorded: ${retainedKb}KB retained in sample list.")
    }

    private fun recordLogSpamBurst() {
        repeat(12) { index ->
            JankHunter.recordLogSpam("sample.logging.checkout_renderer", "SampleLogger.render#$index", 5)
        }
        JankHunter.recordCounter("sample.log_spam.manual_burst.count", 12)
        updateStatus("Log spam burst recorded.")
    }

    private fun recordCustomMetrics() {
        val count = clicks.incrementAndGet()
        JankHunter.recordCounter("sample.checkout.render.count", 1)
        JankHunter.recordGauge("sample.checkout.render_items", 24 + count.toLong())
        JankHunter.recordGauge("sample.checkout.cart_value", 1_990 + count.toLong() * 10)
        updateStatus("Custom counters and gauges recorded.")
    }

    private fun recordCleanObject() {
        JankHunter.withFlow("sample.memory_leak.demo") {
            JankHunter.markFlowStep("clean_object")
            JankHunter.withOwner("sample.memory_leak.cleaned_scope") {
                val probe = CleanedLeakProbe()
                JankHunter.watchObject(
                    probe,
                    "io.jankhunter.sample.CleanedLeakProbe",
                    "sample.memory_leak.cleaned_scope",
                )
                watchWithLeakCanary(probe, "clean object should be collected")
            }
        }
        JankHunter.recordCounter("sample.memory_leak.clean.watch.count", 1)
        updateStatus("Clean object watched without retaining.")
    }

    private fun recordLeakDemo(
        step: String,
        owner: String,
        className: String,
        factory: () -> Any,
    ) {
        JankHunter.withFlow("sample.memory_leak.demo") {
            JankHunter.markFlowStep(step)
            JankHunter.withOwner(owner) {
                val sample = factory()
                retainedSamples += sample
                JankHunter.watchObject(sample, className, owner)
                watchWithLeakCanary(sample, "$step retained by $owner")
            }
        }
        JankHunter.recordCounter("sample.memory_leak.watch.count", 1)
        JankHunter.flush()
        updateStatus("Leak scenario recorded: $step.")
    }

    private fun recordCacheLeaks() {
        JankHunter.withFlow("sample.memory_leak.demo") {
            JankHunter.markFlowStep("cache_entries")
            JankHunter.withOwner("sample.memory_leak.checkout_cache") {
                repeat(3) { index ->
                    val entry = LeakedCacheEntry(index = index, payload = ByteArray(128 * 1024))
                    retainedSamples += entry
                    JankHunter.watchObject(
                        entry,
                        "io.jankhunter.sample.LeakedCheckoutCacheEntry",
                        "sample.memory_leak.checkout_cache",
                    )
                    watchWithLeakCanary(entry, "cache entry $index retained by checkout cache")
                }
            }
        }
        JankHunter.recordCounter("sample.memory_leak.cache.watch.count", 3)
        JankHunter.flush()
        updateStatus("Retained cache entries watched.")
    }

    private fun recordLeakRegressionBurst() {
        JankHunter.withFlow("sample.memory_leak.compare_candidate") {
            repeat(2) { index ->
                JankHunter.markFlowStep("activity_and_binding_burst_$index")
                JankHunter.withOwner("sample.memory_leak.regression_burst") {
                    val activity = LeakedActivityScreen(clicks.incrementAndGet())
                    val binding = LeakedBindingSnapshot(screen = this@MainActivity, payload = ByteArray(160 * 1024))
                    retainedSamples += activity
                    retainedSamples += binding
                    JankHunter.watchObject(
                        activity,
                        "io.jankhunter.sample.LeakedCheckoutActivity",
                        "sample.memory_leak.regression_burst",
                    )
                    watchWithLeakCanary(activity, "regression burst activity $index")
                    JankHunter.watchObject(
                        binding,
                        "io.jankhunter.sample.LeakedCheckoutBinding",
                        "sample.memory_leak.regression_burst",
                    )
                    watchWithLeakCanary(binding, "regression burst binding $index")
                }
            }
        }
        JankHunter.recordCounter("sample.memory_leak.regression_burst.watch.count", 4)
        JankHunter.flush()
        updateStatus("Candidate leak burst watched.")
    }

    private fun runNetworkCall(label: String, owner: String, url: String) {
        updateStatus("Network: $label running.")
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
                runOnUiThread { updateStatus(message) }
            }
        }
    }

    private fun watchWithLeakCanary(watchedObject: Any, description: String) {
        LeakCanaryBridge.watch(watchedObject, description)
        JankHunter.recordCounter("sample.leakcanary.watch.count", 1)
    }

    private fun section(
        title: String,
        subtitle: String,
        beforeActions: List<View> = emptyList(),
        actions: List<View>,
    ): View = card {
        addView(label(title, CYAN))
        addView(copy(subtitle))
        beforeActions.forEach(::addView)
        val grid = LinearLayout(this@MainActivity).apply {
            orientation = LinearLayout.VERTICAL
        }
        actions.forEach { grid.addView(it) }
        addView(grid)
    }

    private fun card(block: LinearLayout.() -> Unit): LinearLayout {
        return LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(16), dp(16), dp(16), dp(16))
            background = rounded(PANEL, CYAN, 1)
            val params = LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.WRAP_CONTENT,
            ).apply {
                bottomMargin = dp(14)
            }
            layoutParams = params
            block()
        }
    }

    private fun action(text: String, accent: Int, onClick: () -> Unit): Button {
        return Button(this).apply {
            this.text = text
            textSize = 14f
            isAllCaps = false
            setTextColor(Color.WHITE)
            background = rounded(accent, accent, 1)
            setPadding(dp(12), dp(8), dp(12), dp(8))
            setOnClickListener { onClick() }
            layoutParams = LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.WRAP_CONTENT,
            ).apply {
                topMargin = dp(8)
            }
        }
    }

    private fun label(text: String, color: Int): TextView {
        return TextView(this).apply {
            this.text = text.uppercase()
            textSize = 11f
            typeface = Typeface.DEFAULT_BOLD
            setTextColor(color)
            letterSpacing = 0f
        }
    }

    private fun title(text: String): TextView {
        return TextView(this).apply {
            this.text = text
            textSize = 28f
            typeface = Typeface.DEFAULT_BOLD
            setTextColor(Color.WHITE)
            setPadding(0, dp(4), 0, dp(6))
        }
    }

    private fun copy(text: String): TextView {
        return TextView(this).apply {
            this.text = text
            textSize = 14f
            setTextColor(MUTED)
            setLineSpacing(0f, 1.08f)
            setPadding(0, 0, 0, dp(8))
        }
    }

    private fun rounded(fill: Int, stroke: Int, strokeWidth: Int): GradientDrawable {
        return GradientDrawable().apply {
            shape = GradientDrawable.RECTANGLE
            cornerRadius = dp(8).toFloat()
            setColor(fill)
            setStroke(dp(strokeWidth), stroke)
        }
    }

    private fun refreshRuntimeFlag(reason: String) {
        if (::runtimeFlagText.isInitialized) {
            val state = if (JankHunter.isRuntimeEnabled()) "ON" else "OFF"
            val active = if (JankHunter.isStarted()) "collecting" else "paused"
            runtimeFlagText.text = "Runtime flag: $state · $active · $reason"
        }
        updateStatus("Runtime flag ${if (JankHunter.isRuntimeEnabled()) "ON" else "OFF"}: $reason")
    }

    private fun refreshLeakCanaryStatus(reason: String) {
        if (::leakCanaryStatusText.isInitialized) {
            leakCanaryStatusText.text = "${LeakCanaryBridge.status()} · $reason"
        }
    }

    private fun updateStatus(message: String) {
        if (::statusText.isInitialized) {
            statusText.text = message
        }
    }

    private fun dp(value: Int): Int = (value * resources.displayMetrics.density).toInt()

    private data class LeakedActivityScreen(val id: Int)

    private data class LeakedBindingSnapshot(
        val screen: Activity,
        val payload: ByteArray,
    )

    private class LeakedPaymentListener(
        val onPaymentStateChanged: () -> Unit,
    )

    private data class LeakedCacheEntry(
        val index: Int,
        val payload: ByteArray,
    )

    private class CleanedLeakProbe

    private companion object {
        val BG: Int = Color.rgb(7, 10, 18)
        val PANEL: Int = Color.rgb(12, 18, 34)
        val CYAN: Int = Color.rgb(111, 247, 255)
        val OK: Int = Color.rgb(47, 166, 111)
        val WARN: Int = Color.rgb(176, 132, 31)
        val BAD: Int = Color.rgb(186, 54, 84)
        val MAGENTA: Int = Color.rgb(156, 70, 190)
        val MUTED: Int = Color.rgb(164, 183, 201)
    }
}
