package io.jankhunter.runtime

import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.io.WorkSubmissionObservation
import io.jankhunter.runtime.internal.io.WorkSubmissionPolicy
import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class WorkScheduleArtTest {
    @Test fun activeFacadeCapturesSchedulesAndDisabledEpochDoesNotAcceptRequests() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val context = instrumentation.targetContext
        val root = File(context.cacheDir, "work-schedule-active").apply { deleteRecursively() }
        JankHunter.shutdown()
        try {
            JankHunter.init(context, JankHunterConfig.builder().autoStartCollectors(false)
                .workerTracingEnabled(true).runtimeCallGraphEnabled(false).logDirectory(root).build())
            assertTrue(JankHunter.isStarted())
            recordFacadeRequest(30, "active-first")
            assertTrue(JankHunter.setRuntimeEnabled(false))
            recordFacadeRequest(35, "disabled")
            assertTrue(JankHunter.setRuntimeEnabled(true))
            assertTrue(JankHunter.isStarted())
            recordFacadeRequest(40, "active-second")
        } finally { JankHunter.shutdown() }
        val log = root.walkTopDown().single { it.isFile && it.extension == "jhlog" }
        log.copyTo(File(instrumentation.context.filesDir, "work-schedule-active-art.jhlog"), overwrite = true)
        root.deleteRecursively()
    }

    private fun recordFacadeRequest(minutes: Long, name: String) {
        val builder = androidx.work.PeriodicWorkRequest.Builder(androidx.work.Worker::class.java,
            minutes, java.util.concurrent.TimeUnit.MINUTES, 5, java.util.concurrent.TimeUnit.MINUTES)
            .setInitialDelay(0, java.util.concurrent.TimeUnit.MILLISECONDS)
        JankHunterHooks.observeWorkBuilderPeriodCall(builder, minutes, java.util.concurrent.TimeUnit.MINUTES,
            5, java.util.concurrent.TimeUnit.MINUTES)
        JankHunterHooks.observeWorkBuilderDelayCall(builder, 0, java.util.concurrent.TimeUnit.MILLISECONDS)
        val request = builder.build()
        JankHunterHooks.observeWorkBuilderBuildCall(builder, request)
        JankHunterHooks.observeWorkSubmissionRequests(request, name, androidx.work.ExistingPeriodicWorkPolicy.UPDATE,
            "app.Schedule.create")
    }

    @Test fun scheduleHintsSurviveBuilderReuseAndSerializeOnArt() {
        val tracker = RuntimeWorkBuilderTracker()
        val builder = Any()
        assertTrue(tracker.periodic(builder, 1_800_000, 300_000))
        assertTrue(tracker.initialDelay(builder, 0))
        assertTrue(tracker.built(builder, 1, 2))
        val first = checkNotNull(tracker.find(1, 2))
        assertTrue(tracker.initialDelay(builder, 600_000))
        assertTrue(tracker.built(builder, 3, 4))
        assertEquals(0L, first.initialDelayMS)
        assertEquals(600_000L, tracker.find(3, 4)?.initialDelayMS)
        assertNull(tracker.find(5, 6))
        val context = InstrumentationRegistry.getInstrumentation().context
        val root = File(context.cacheDir, "work-schedule-wire").apply { deleteRecursively() }
        val writer = AsyncLogWriterFactory().open(root, JankHunterConfig.builder().autoStartCollectors(false).build(), "schedule-art")
        try {
            writer.workSubmission(WorkSubmissionObservation(1, 2, 3, true, "config", WorkSubmissionPolicy.UPDATE, 0,
                callerSource = "app.Schedule.create", requestedPeriodMS = first.periodMS,
                requestedFlexMS = first.flexMS, requestedInitialDelayMS = first.initialDelayMS))
        } finally { writer.close() }
        val log = root.walkTopDown().single { it.isFile && it.extension == "jhlog" }
        log.copyTo(File(context.filesDir, "work-schedule-art.jhlog"), overwrite = true)
        root.deleteRecursively()
    }
}
