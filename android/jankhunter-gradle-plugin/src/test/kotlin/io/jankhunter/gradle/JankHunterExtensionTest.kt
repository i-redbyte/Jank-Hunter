package io.jankhunter.gradle

import org.gradle.api.Project
import org.gradle.testfixtures.ProjectBuilder
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Test

class JankHunterExtensionTest {
    @Test
    fun symbolsAreEmbeddedByDefaultAndExternalModeIsExplicit() {
        val extension = extension()

        assertEquals(JankHunterSymbolMode.EMBEDDED, extension.symbolMode.get())

        extension.symbolMode.set(JankHunterSymbolMode.STABLE_EXTERNAL)

        assertEquals(JankHunterSymbolMode.STABLE_EXTERNAL, extension.symbolMode.get())
    }

    @Test
    fun dependencyInjectionAnalysisRequiresExplicitOptIn() {
        val extension = extension()

        assertEquals(JankHunterFeatureMode.DISABLED, extension.dependencyInjectionAnalysis.get())

        extension.dependencyInjectionAnalysis.set(JankHunterFeatureMode.ENABLED)

        assertEquals(JankHunterFeatureMode.ENABLED, extension.dependencyInjectionAnalysis.get())
    }

    @Test
    fun enabledAcceptsProviderBackedKillSwitch() {
        val project = project()
        val extension = extension(project)

        assertEquals(true, extension.enabled.get())

        extension.enabled.set(project.providers.provider { false })

        assertEquals(false, extension.enabled.get())
    }

    @Test
    fun instrumentationDslAcceptsNormalizedPackageSets() {
        val instrumentation = extension().instrument

        instrumentation.includePackages("com.myapp", " com.myapp.feature ")
        instrumentation.includePackages(listOf("com.myapp.data", ""))
        instrumentation.excludePackages("com.myapp.generated", "com.myapp.di")
        instrumentation.excludePackages(listOf("com.myapp.legacy"))
        instrumentation.asmProgressLog.set(true)
        instrumentation.classGraph.set(true)
        instrumentation.runtimeCallGraph.set(true)

        assertEquals(
            linkedSetOf("com.myapp", "com.myapp.feature", "com.myapp.data"),
            instrumentation.includePackages.get(),
        )
        assertEquals(
            linkedSetOf("com.myapp.generated", "com.myapp.di", "com.myapp.legacy"),
            instrumentation.excludePackages.get(),
        )
        assertEquals(true, instrumentation.asmProgressLog.get())
        assertEquals(true, instrumentation.classGraph.get())
        assertEquals(true, instrumentation.runtimeCallGraph.get())
    }

    @Test
    fun instrumentationDefaultsToBoundedDiagnostics() {
        val instrumentation = extension().instrument

        assertEquals(false, instrumentation.asmProgressLog.get())
        assertEquals(true, instrumentation.classGraph.get())
        assertEquals(false, instrumentation.runtimeCallGraph.get())
        assertEquals(false, instrumentation.okhttp.get())
        assertEquals(false, instrumentation.webSockets.get())
        assertEquals(true, instrumentation.handlers.get())
        assertEquals(true, instrumentation.executors.get())
        assertEquals(false, instrumentation.coroutines.get())
        assertEquals(false, instrumentation.methodCounters.get())
        assertEquals(true, instrumentation.lifecycleLeaks.get())
        assertEquals(emptySet<String>(), instrumentation.includePackages.get())
    }

    @Test
    fun instrumentationDslDoesNotExposeRemovedOrDisconnectedFlags() {
        val methodNames = JankHunterExtension.Instrumentation::class.java.methods
            .mapTo(mutableSetOf()) { it.name }

        assertFalse("getActivities" in methodNames)
        assertFalse("getFragments" in methodNames)
        assertFalse("getRxJava" in methodNames)
        assertFalse("getAllowEmptyIncludePackages" in methodNames)
        assertFalse("getIncludeWholeApplication" in methodNames)
    }

    @Test
    fun extensionDoesNotExposeDependencyManagementBlock() {
        val methodNames = JankHunterExtension::class.java.methods.mapTo(mutableSetOf()) { it.name }

        assertFalse("dependencies" in methodNames)
        assertFalse("getDependencies" in methodNames)
    }

    @Test
    fun retainedHeapDumpDslRequiresExplicitOptInByDefault() {
        val retainedHeapDump = extension().retainedHeapDump

        assertEquals(false, retainedHeapDump.enabled.get())
        assertEquals(false, retainedHeapDump.privacyApproved.get())
        assertEquals(10 * 60_000L, retainedHeapDump.minIntervalMs.get())
        assertEquals(1, retainedHeapDump.maxCount.get())
        assertEquals(30_000L, retainedHeapDump.minRetainedAgeMs.get())
    }

    @Test
    fun sessionLogSizeLimitUsesExplicitMiBDsl() {
        val extension = extension()

        assertEquals(true, extension.sessionLogSizeLimitEnabled.get())
        assertEquals(16, extension.maxSessionLogSizeMiB.get())

        extension.sessionLogSizeLimitEnabled.set(false)
        extension.maxSessionLogSizeMiB.set(32)

        assertEquals(false, extension.sessionLogSizeLimitEnabled.get())
        assertEquals(32, extension.maxSessionLogSizeMiB.get())

        val methodNames = JankHunterExtension::class.java.methods.mapTo(mutableSetOf()) { it.name }
        assertFalse("getMaxSessionLogBytes" in methodNames)
        assertFalse("getMaxSessionLogsBytes" in methodNames)
    }

    @Test
    fun runtimeDslExposesScenarioThresholdsAndCollectors() {
        val extension = extension()

        assertEquals(700L, extension.runtime.mainThreadStallThresholdMs.get())
        assertEquals(250L, extension.runtime.ownerBlockThresholdMs.get())
        assertEquals(1_000L, extension.runtime.httpSlowThresholdMs.get())
        assertEquals(32L, extension.runtime.jankFrameThresholdMs.get())
        assertEquals(32L, extension.runtime.uiWindowP95ThresholdMs.get())
        assertEquals(false, extension.runtime.mainLooperDispatchMonitor.get())
        assertEquals(true, extension.runtime.jankStats.get())
        assertEquals(true, extension.runtime.mainProcessOnly.get())

        extension.runtime {
            it.mainThreadStallThresholdMs.set(900L)
            it.ownerBlockThresholdMs.set(120L)
            it.httpSlowThresholdMs.set(2_000L)
            it.jankFrameThresholdMs.set(24L)
            it.uiWindowP95ThresholdMs.set(40L)
            it.mainLooperDispatchMonitor.set(false)
            it.jankStats.set(false)
            it.mainProcessOnly.set(false)
        }

        assertEquals(900L, extension.runtime.mainThreadStallThresholdMs.get())
        assertEquals(120L, extension.runtime.ownerBlockThresholdMs.get())
        assertEquals(2_000L, extension.runtime.httpSlowThresholdMs.get())
        assertEquals(24L, extension.runtime.jankFrameThresholdMs.get())
        assertEquals(40L, extension.runtime.uiWindowP95ThresholdMs.get())
        assertEquals(false, extension.runtime.mainLooperDispatchMonitor.get())
        assertEquals(false, extension.runtime.jankStats.get())
        assertEquals(false, extension.runtime.mainProcessOnly.get())
    }

    @Test
    fun retainedHeapDumpDslAcceptsRuntimeLimits() {
        val extension = extension()

        extension.retainedHeapDump {
            it.enabled.set(true)
            it.privacyApproved.set(true)
            it.minIntervalMs.set(123_000L)
            it.maxCount.set(2)
            it.minRetainedAgeMs.set(45_000L)
        }

        assertEquals(true, extension.retainedHeapDump.enabled.get())
        assertEquals(true, extension.retainedHeapDump.privacyApproved.get())
        assertEquals(123_000L, extension.retainedHeapDump.minIntervalMs.get())
        assertEquals(2, extension.retainedHeapDump.maxCount.get())
        assertEquals(45_000L, extension.retainedHeapDump.minRetainedAgeMs.get())
    }

    @Test
    fun releaseSafetyDslIsExplicitByDefault() {
        val releaseSafety = extension().releaseSafety

        assertEquals(false, releaseSafety.allowInstrumentation.get())
        assertEquals(false, releaseSafety.privacyReviewed.get())
        assertEquals(false, releaseSafety.allowHeapDumps.get())
        assertEquals(false, releaseSafety.allowSecondaryProcesses.get())
        assertNull(releaseSafety.performanceBudgetEvidence.orNull)
    }

    private fun extension(project: Project = project()): JankHunterExtension {
        return project.objects.newInstance(JankHunterExtension::class.java)
    }

    private fun project(): Project = ProjectBuilder.builder().build()
}
