package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test

class JankHunterExtensionTest {
    @Test
    fun extensionDslCanDisablePluginForBuild() {
        val extension = JankHunterExtension()

        assertEquals(true, extension.enabled)

        extension.enabled = false

        assertEquals(false, extension.enabled)
    }

    @Test
    fun instrumentationDslAcceptsPackageLists() {
        val instrumentation = JankHunterExtension.Instrumentation()

        instrumentation.includePackages("com.myapp", " com.myapp.feature ")
        instrumentation.includePackages(listOf("com.myapp.data", ""))
        instrumentation.excludePackages("com.myapp.generated", "com.myapp.di")
        instrumentation.excludePackages(listOf("com.myapp.legacy"))
        instrumentation.includeWholeApplication = true
        instrumentation.asmProgressLog = true
        instrumentation.classGraph = true
        instrumentation.runtimeCallGraph = true

        assertEquals(
            linkedSetOf("com.myapp", "com.myapp.feature", "com.myapp.data"),
            instrumentation.includePackages,
        )
        assertEquals(
            linkedSetOf("com.myapp.generated", "com.myapp.di", "com.myapp.legacy"),
            instrumentation.excludePackages,
        )
        assertEquals(true, instrumentation.includeWholeApplication)
        assertEquals(true, instrumentation.asmProgressLog)
        assertEquals(true, instrumentation.classGraph)
        assertEquals(true, instrumentation.runtimeCallGraph)
    }

    @Test
    fun instrumentationDefaultsToBroadDiagnostics() {
        val instrumentation = JankHunterExtension.Instrumentation()

        assertEquals(false, instrumentation.includeWholeApplication)
        assertEquals(false, instrumentation.asmProgressLog)
        assertEquals(true, instrumentation.classGraph)
        assertEquals(true, instrumentation.runtimeCallGraph)
        assertEquals(true, instrumentation.coroutines)
        assertEquals(true, instrumentation.allowEmptyIncludePackages)
        assertEquals(false, instrumentation.methodCounters)
        assertEquals(true, instrumentation.lifecycleLeaks)
    }

    @Test
    fun instrumentationDslDoesNotExposeDisconnectedFlags() {
        val methodNames = JankHunterExtension.Instrumentation::class.java.methods
            .mapTo(mutableSetOf()) { it.name }

        assertFalse("getActivities" in methodNames)
        assertFalse("setActivities" in methodNames)
        assertFalse("getFragments" in methodNames)
        assertFalse("setFragments" in methodNames)
        assertFalse("getRxJava" in methodNames)
        assertFalse("setRxJava" in methodNames)
    }

    @Test
    fun retainedHeapDumpDslIsEnabledByDefaultForDiagnostics() {
        val retainedHeapDump = JankHunterExtension.RetainedHeapDump()

        assertEquals(true, retainedHeapDump.enabled)
        assertEquals(true, retainedHeapDump.privacyApproved)
        assertEquals(10 * 60_000L, retainedHeapDump.minIntervalMs)
        assertEquals(1, retainedHeapDump.maxCount)
        assertEquals(30_000L, retainedHeapDump.minRetainedAgeMs)
    }

    @Test
    fun logBucketDefaultsToOneRuntimeSession() {
        val extension = JankHunterExtension()

        assertEquals("session", extension.logBucket)

        extension.logBucket = "daily"

        assertEquals("daily", extension.logBucket)
    }

    @Test
    fun runtimeDslExposesScenarioThresholdsAndCollectors() {
        val extension = JankHunterExtension()

        assertEquals(700L, extension.runtime.mainThreadStallThresholdMs)
        assertEquals(250L, extension.runtime.ownerBlockThresholdMs)
        assertEquals(1_000L, extension.runtime.httpSlowThresholdMs)
        assertEquals(32L, extension.runtime.jankFrameThresholdMs)
        assertEquals(32L, extension.runtime.uiWindowP95ThresholdMs)
        assertEquals(true, extension.runtime.mainLooperDispatchMonitor)
        assertEquals(true, extension.runtime.jankStats)
        assertEquals(true, extension.runtime.mainProcessOnly)

        extension.runtime {
            it.mainThreadStallThresholdMs = 900L
            it.ownerBlockThresholdMs = 120L
            it.httpSlowThresholdMs = 2_000L
            it.jankFrameThresholdMs = 24L
            it.uiWindowP95ThresholdMs = 40L
            it.mainLooperDispatchMonitor = false
            it.jankStats = false
            it.mainProcessOnly = false
        }

        assertEquals(900L, extension.runtime.mainThreadStallThresholdMs)
        assertEquals(120L, extension.runtime.ownerBlockThresholdMs)
        assertEquals(2_000L, extension.runtime.httpSlowThresholdMs)
        assertEquals(24L, extension.runtime.jankFrameThresholdMs)
        assertEquals(40L, extension.runtime.uiWindowP95ThresholdMs)
        assertEquals(false, extension.runtime.mainLooperDispatchMonitor)
        assertEquals(false, extension.runtime.jankStats)
        assertEquals(false, extension.runtime.mainProcessOnly)
    }

    @Test
    fun retainedHeapDumpDslAcceptsRuntimeLimits() {
        val extension = JankHunterExtension()

        extension.retainedHeapDump {
            it.enabled = true
            it.privacyApproved = true
            it.minIntervalMs = 123_000L
            it.maxCount = 2
            it.minRetainedAgeMs = 45_000L
        }

        assertEquals(true, extension.retainedHeapDump.enabled)
        assertEquals(true, extension.retainedHeapDump.privacyApproved)
        assertEquals(123_000L, extension.retainedHeapDump.minIntervalMs)
        assertEquals(2, extension.retainedHeapDump.maxCount)
        assertEquals(45_000L, extension.retainedHeapDump.minRetainedAgeMs)
    }

    @Test
    fun releaseSafetyDslIsExplicitByDefault() {
        val releaseSafety = JankHunterExtension.ReleaseSafety()

        assertEquals(false, releaseSafety.allowInstrumentation)
        assertEquals(false, releaseSafety.allowDependencyInstrumentation)
        assertEquals(false, releaseSafety.privacyReviewed)
        assertEquals(false, releaseSafety.allowDeviceInfo)
        assertEquals(false, releaseSafety.allowHeapDumps)
        assertEquals(false, releaseSafety.allowSecondaryProcesses)
        assertEquals(null, releaseSafety.performanceBudgetEvidence)
    }
}
