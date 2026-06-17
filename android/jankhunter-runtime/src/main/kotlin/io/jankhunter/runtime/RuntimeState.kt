package io.jankhunter.runtime

import android.app.Application
import android.content.Context
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.MetricAggregator
import io.jankhunter.runtime.internal.system.AdaptiveRuntimeSampler
import io.jankhunter.runtime.internal.system.ActivityTracker
import io.jankhunter.runtime.internal.system.FpsMonitor
import io.jankhunter.runtime.internal.system.MainLooperDispatchMonitor
import io.jankhunter.runtime.internal.system.MainThreadWatchdog
import io.jankhunter.runtime.internal.system.MemorySampler
import io.jankhunter.runtime.internal.system.MemoryTrimReporter
import io.jankhunter.runtime.internal.system.ObjectRetentionWatcher
import io.jankhunter.runtime.internal.system.RetainedHeapDumper
import io.jankhunter.runtime.internal.system.SystemContextSampler
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong

internal class RuntimeState(defaultMaxMetricAggregationKeys: Int) {
    val started = AtomicBoolean(false)
    val initAttempts = AtomicLong()
    val initFailures = AtomicLong()

    @Volatile
    var writer: AsyncLogWriter? = null

    @Volatile
    var config: JankHunterConfig? = null

    @Volatile
    var metricAggregator = MetricAggregator(defaultMaxMetricAggregationKeys)

    @Volatile
    var watchdog: MainThreadWatchdog? = null

    @Volatile
    var dispatchMonitor: MainLooperDispatchMonitor? = null

    @Volatile
    var memorySampler: MemorySampler? = null

    @Volatile
    var memoryTrimReporter: MemoryTrimReporter? = null

    @Volatile
    var componentCallbackContext: Context? = null

    @Volatile
    var systemContextSampler: SystemContextSampler? = null

    @Volatile
    var adaptiveRuntimeSampler: AdaptiveRuntimeSampler? = null

    @Volatile
    var objectRetentionWatcher: ObjectRetentionWatcher? = null

    @Volatile
    var retainedHeapDumper: RetainedHeapDumper? = null

    @Volatile
    var fpsMonitor: FpsMonitor? = null

    @Volatile
    var application: Application? = null

    @Volatile
    var activityTracker: ActivityTracker? = null

    @Volatile
    var initDiagnostics = JankHunterInitDiagnostics(status = "not_started")
}
