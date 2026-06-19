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
        refreshRuntimeFlag(getString(R.string.status_ready))
        refreshLeakCanaryStatus(getString(R.string.status_ready))
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
            text = getString(R.string.hero_status_ready)
            textSize = 14f
            setTextColor(MUTED)
        }
        return card {
            addView(label(getString(R.string.hero_label), CYAN))
            addView(title(getString(R.string.hero_title)))
            addView(copy(getString(R.string.hero_description)))
            addView(statusText)
        }
    }

    private fun controlTower(): View = section(
        title = getString(R.string.section_control_tower),
        subtitle = getString(R.string.section_control_tower_subtitle),
        actions = listOf(
            action(getString(R.string.action_run_clean_baseline), OK) { runCleanBaseline() },
            action(getString(R.string.action_run_noisy_candidate), MAGENTA) { runNoisyCandidate() },
            action(getString(R.string.action_flush_diagnostics), CYAN) {
                JankHunter.flush()
                updateStatus(getString(R.string.status_flushed))
            },
        ),
    )

    private fun featureFlagLab(): View {
        runtimeFlagText = TextView(this).apply {
            textSize = 14f
            setTextColor(MUTED)
        }
        return section(
            title = getString(R.string.section_feature_flag),
            subtitle = getString(R.string.section_feature_flag_subtitle),
            beforeActions = listOf(runtimeFlagText),
            actions = listOf(
                action(getString(R.string.action_enable_sdk_runtime), OK) {
                    val enabled = JankHunter.setRuntimeEnabled(true, "sample_feature_flag")
                    JankHunter.setScreen("SamplePlayground")
                    refreshRuntimeFlag(
                        if (enabled) {
                            getString(R.string.status_runtime_enabled)
                        } else {
                            getString(R.string.status_runtime_enable_failed)
                        },
                    )
                },
                action(getString(R.string.action_disable_sdk_runtime), WARN) {
                    JankHunter.setRuntimeEnabled(false, "sample_feature_flag")
                    refreshRuntimeFlag(getString(R.string.status_runtime_disabled))
                },
                action(getString(R.string.action_record_flag_probe), CYAN) {
                    JankHunter.recordCounter("sample.feature_flag.probe.count", 1)
                    JankHunter.recordGauge("sample.feature_flag.runtime_enabled", if (JankHunter.isRuntimeEnabled()) 1 else 0)
                    refreshRuntimeFlag(getString(R.string.status_runtime_probe_recorded))
                },
            ),
        )
    }

    private fun performanceLab(): View = section(
        title = getString(R.string.section_performance_lab),
        subtitle = getString(R.string.section_performance_lab_subtitle),
        actions = listOf(
            action(getString(R.string.action_ui_stall), WARN) { recordUiStall() },
            action(getString(R.string.action_background_work), CYAN) { recordBackgroundWork() },
            action(getString(R.string.action_http_success), OK) {
                runNetworkCall(
                    label = "JSONPlaceholder",
                    owner = "sample.network.jsonplaceholder",
                    url = "https://jsonplaceholder.typicode.com/posts/1",
                )
            },
            action(getString(R.string.action_http_503), BAD) {
                runNetworkCall(
                    label = "httpbin 503",
                    owner = "sample.network.httpbin_503",
                    url = "https://httpbin.org/status/503",
                )
            },
            action(getString(R.string.action_memory_pressure), MAGENTA) { recordMemoryPressure() },
            action(getString(R.string.action_log_spam), BAD) { recordLogSpamBurst() },
            action(getString(R.string.action_custom_metrics), CYAN) { recordCustomMetrics() },
        ),
    )

    private fun leakLab(): View = section(
        title = getString(R.string.section_leak_lab),
        subtitle = getString(R.string.section_leak_lab_subtitle),
        actions = listOf(
            action(getString(R.string.action_clean_object), OK) { recordCleanObject() },
            action(getString(R.string.action_activity_reference), MAGENTA) {
                recordLeakDemo(
                    step = "activity_reference",
                    displayName = getString(R.string.leak_display_activity_reference),
                    owner = "sample.memory_leak.activity_registry",
                    className = "io.jankhunter.sample.LeakedCheckoutActivity",
                ) {
                    LeakedActivityScreen(clicks.incrementAndGet())
                }
            },
            action(getString(R.string.action_view_binding), MAGENTA) {
                recordLeakDemo(
                    step = "view_binding",
                    displayName = getString(R.string.leak_display_view_binding),
                    owner = "sample.memory_leak.binding_cache",
                    className = "io.jankhunter.sample.LeakedCheckoutBinding",
                ) {
                    LeakedBindingSnapshot(screen = this@MainActivity, payload = ByteArray(96 * 1024))
                }
            },
            action(getString(R.string.action_listener_callback), WARN) {
                recordLeakDemo(
                    step = "listener_callback",
                    displayName = getString(R.string.leak_display_listener_callback),
                    owner = "sample.memory_leak.listener_registry",
                    className = "io.jankhunter.sample.LeakedPaymentListener",
                ) {
                    LeakedPaymentListener {
                        updateStatus(getString(R.string.status_listener_retained))
                    }
                }
            },
            action(getString(R.string.action_cache_entries), WARN) { recordCacheLeaks() },
            action(getString(R.string.action_clear_retained_list), OK) {
                retainedSamples.clear()
                JankHunter.recordCounter("sample.memory_leak.retained_list.clear.count", 1)
                updateStatus(getString(R.string.status_retained_list_cleared))
            },
        ),
    )

    private fun leakCanaryBenchmarkLab(): View {
        leakCanaryStatusText = TextView(this).apply {
            textSize = 14f
            setTextColor(MUTED)
        }
        return section(
            title = getString(R.string.section_leakcanary_benchmark),
            subtitle = getString(R.string.section_leakcanary_benchmark_subtitle),
            beforeActions = listOf(leakCanaryStatusText),
            actions = listOf(
                action(getString(R.string.action_both_clean_object), OK) {
                    recordCleanObject()
                    refreshLeakCanaryStatus(getString(R.string.status_clean_object_queued))
                },
                action(getString(R.string.action_both_retained_object), MAGENTA) {
                    recordLeakDemo(
                        step = "leakcanary_retained_object",
                        displayName = getString(R.string.leak_display_leakcanary_retained),
                        owner = "sample.memory_leak.leakcanary_benchmark",
                        className = "io.jankhunter.sample.LeakedCheckoutActivity",
                    ) {
                        LeakedActivityScreen(clicks.incrementAndGet())
                    }
                    refreshLeakCanaryStatus(getString(R.string.status_retained_object_queued))
                },
                action(getString(R.string.action_both_cache_burst), BAD) {
                    recordCacheLeaks()
                    refreshLeakCanaryStatus(getString(R.string.status_cache_burst_queued))
                },
                action(getString(R.string.action_how_to_compare), CYAN) {
                    JankHunter.flush()
                    updateStatus(getString(R.string.status_compare_hint))
                    refreshLeakCanaryStatus(getString(R.string.status_comparison_hint_shown))
                },
            ),
        )
    }

    private fun compareLab(): View = section(
        title = getString(R.string.section_compare_lab),
        subtitle = getString(R.string.section_compare_lab_subtitle),
        actions = listOf(
            action(getString(R.string.action_candidate_leak_burst), BAD) { recordLeakRegressionBurst() },
            action(getString(R.string.action_candidate_perf_burst), BAD) {
                repeat(2) { recordUiStall() }
                recordMemoryPressure()
                recordLogSpamBurst()
                updateStatus(getString(R.string.status_candidate_perf_burst_recorded))
            },
            action(getString(R.string.action_pull_report_hint), CYAN) {
                JankHunter.flush()
                updateStatus(getString(R.string.status_pull_report_hint))
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
        updateStatus(getString(R.string.status_baseline_recorded))
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
        updateStatus(getString(R.string.status_candidate_recorded))
    }

    private fun recordUiStall() {
        val count = clicks.incrementAndGet()
        JankHunter.withOwner("sample.ui.synthetic_stall") {
            SystemClock.sleep(280)
        }
        JankHunter.recordCounter("sample.ui_stall.clicks", 1)
        updateStatus(getString(R.string.status_ui_stall_recorded, count))
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
            runOnUiThread { updateStatus(getString(R.string.status_background_work_recorded)) }
        }
    }

    private fun recordMemoryPressure() {
        val chunk = ByteArray(384 * 1024)
        memoryPressure += chunk
        val retainedKb = memoryPressure.sumOf { it.size / 1024L }
        JankHunter.recordGauge("sample.memory.pressure_kb", retainedKb)
        JankHunter.recordCounter("sample.memory.pressure.alloc.count", 1)
        updateStatus(getString(R.string.status_memory_pressure_recorded, retainedKb))
    }

    private fun recordLogSpamBurst() {
        repeat(12) { index ->
            JankHunter.recordLogSpam("sample.logging.checkout_renderer", "SampleLogger.render#$index", 5)
        }
        JankHunter.recordCounter("sample.log_spam.manual_burst.count", 12)
        updateStatus(getString(R.string.status_log_spam_recorded))
    }

    private fun recordCustomMetrics() {
        val count = clicks.incrementAndGet()
        JankHunter.recordCounter("sample.checkout.render.count", 1)
        JankHunter.recordGauge("sample.checkout.render_items", 24 + count.toLong())
        JankHunter.recordGauge("sample.checkout.cart_value", 1_990 + count.toLong() * 10)
        updateStatus(getString(R.string.status_custom_metrics_recorded))
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
                watchWithLeakCanary(probe, getString(R.string.leakcanary_desc_clean_object))
            }
        }
        JankHunter.recordCounter("sample.memory_leak.clean.watch.count", 1)
        updateStatus(getString(R.string.status_clean_object_watched))
    }

    private fun recordLeakDemo(
        step: String,
        displayName: String,
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
                watchWithLeakCanary(
                    sample,
                    getString(R.string.leakcanary_desc_retained_by_owner, displayName, owner),
                )
            }
        }
        JankHunter.recordCounter("sample.memory_leak.watch.count", 1)
        JankHunter.flush()
        updateStatus(getString(R.string.status_leak_scenario_recorded, displayName))
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
                    watchWithLeakCanary(entry, getString(R.string.leakcanary_desc_cache_entry, index))
                }
            }
        }
        JankHunter.recordCounter("sample.memory_leak.cache.watch.count", 3)
        JankHunter.flush()
        updateStatus(getString(R.string.status_cache_entries_watched))
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
                    watchWithLeakCanary(activity, getString(R.string.leakcanary_desc_regression_activity, index))
                    JankHunter.watchObject(
                        binding,
                        "io.jankhunter.sample.LeakedCheckoutBinding",
                        "sample.memory_leak.regression_burst",
                    )
                    watchWithLeakCanary(binding, getString(R.string.leakcanary_desc_regression_binding, index))
                }
            }
        }
        JankHunter.recordCounter("sample.memory_leak.regression_burst.watch.count", 4)
        JankHunter.flush()
        updateStatus(getString(R.string.status_candidate_leak_burst_watched))
    }

    private fun runNetworkCall(label: String, owner: String, url: String) {
        updateStatus(getString(R.string.status_network_running, label))
        executor.execute {
            val startedAt = SystemClock.elapsedRealtime()
            var message = getString(R.string.status_network_failed, label)

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
                message = getString(R.string.status_network_http, label, responseCode, responseBytes)
            } catch (throwable: Throwable) {
                JankHunter.recordCounter("sample.network.failure.count", 1)
                message = getString(R.string.status_network_exception, label, throwable.javaClass.simpleName)
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
            val state = if (JankHunter.isRuntimeEnabled()) {
                getString(R.string.runtime_flag_on)
            } else {
                getString(R.string.runtime_flag_off)
            }
            val active = if (JankHunter.isStarted()) {
                getString(R.string.runtime_collecting)
            } else {
                getString(R.string.runtime_paused)
            }
            runtimeFlagText.text = getString(R.string.runtime_flag_line, state, active, reason)
        }
        val state = if (JankHunter.isRuntimeEnabled()) {
            getString(R.string.runtime_flag_on)
        } else {
            getString(R.string.runtime_flag_off)
        }
        updateStatus(getString(R.string.runtime_status_line, state, reason))
    }

    private fun refreshLeakCanaryStatus(reason: String) {
        if (::leakCanaryStatusText.isInitialized) {
            leakCanaryStatusText.text = getString(
                R.string.leakcanary_status_line,
                LeakCanaryBridge.status(this),
                reason,
            )
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
