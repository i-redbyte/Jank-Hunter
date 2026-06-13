package io.jankhunter.sample

import android.app.Activity
import android.os.Bundle
import android.os.SystemClock
import android.view.Gravity
import android.view.ViewGroup
import android.widget.Button
import android.widget.LinearLayout
import android.widget.TextView
import io.jankhunter.runtime.JankHunter
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicInteger

class MainActivity : Activity() {
    private val executor = Executors.newSingleThreadExecutor { runnable ->
        Thread(runnable, "JankHunterSampleWorker")
    }
    private val clicks = AtomicInteger()
    private val retainedSamples = mutableListOf<Any>()

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
                JankHunter.watchObject(sample, "io.jankhunter.sample.RetainedSample")
                JankHunter.recordCounter("sample.retained.watch.count", 1)
            }
        }

        return LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER_HORIZONTAL
            setPadding(padding, padding, padding, padding)
            addView(title, ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
            addView(status, ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
            addView(jankButton, ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
            addView(workerButton, ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
            addView(watchButton, ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
        }
    }

    private data class RetainedSample(val id: Int)
}
