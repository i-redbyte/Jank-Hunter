package io.jankhunter.gradle

import org.gradle.api.Action
import org.gradle.api.model.ObjectFactory
import org.gradle.api.provider.Property
import org.gradle.api.provider.SetProperty
import javax.inject.Inject

open class JankHunterExtension @Inject constructor(objects: ObjectFactory) {
    val enabled: Property<Boolean> = objects.property(Boolean::class.java).convention(true)
    val enabledBuildTypes: SetProperty<String> = objects.setProperty(String::class.java).convention(setOf("debug"))
    val dependencyInjectionAnalysis: Property<JankHunterFeatureMode> =
        objects.property(JankHunterFeatureMode::class.java).convention(JankHunterFeatureMode.DISABLED)
    val autoInit: Property<Boolean> = objects.property(Boolean::class.java).convention(true)
    val sessionLogSizeLimitEnabled: Property<Boolean> =
        objects.property(Boolean::class.java).convention(true)
    val maxSessionLogSizeMiB: Property<Int> = objects.property(Int::class.java).convention(16)
    val verboseLogs: Property<Boolean> = objects.property(Boolean::class.java).convention(false)
    val symbolMode: Property<JankHunterSymbolMode> =
        objects.property(JankHunterSymbolMode::class.java).convention(JankHunterSymbolMode.EMBEDDED)

    val runtime: Runtime = objects.newInstance(Runtime::class.java)
    val instrument: Instrumentation = objects.newInstance(Instrumentation::class.java)
    val retainedHeapDump: RetainedHeapDump = objects.newInstance(RetainedHeapDump::class.java)
    val releaseSafety: ReleaseSafety = objects.newInstance(ReleaseSafety::class.java)

    fun runtime(action: Action<Runtime>) {
        action.execute(runtime)
    }

    fun instrument(action: Action<Instrumentation>) {
        action.execute(instrument)
    }

    fun retainedHeapDump(action: Action<RetainedHeapDump>) {
        action.execute(retainedHeapDump)
    }

    fun releaseSafety(action: Action<ReleaseSafety>) {
        action.execute(releaseSafety)
    }

    open class Instrumentation @Inject constructor(objects: ObjectFactory) {
        val okhttp: Property<Boolean> = objects.property(Boolean::class.java).convention(false)
        val webSockets: Property<Boolean> = objects.property(Boolean::class.java).convention(false)
        val handlers: Property<Boolean> = objects.property(Boolean::class.java).convention(true)
        val executors: Property<Boolean> = objects.property(Boolean::class.java).convention(true)
        val coroutines: Property<Boolean> = objects.property(Boolean::class.java).convention(false)
        val flowInteractions: Property<Boolean> = objects.property(Boolean::class.java).convention(true)
        val lifecycleLeaks: Property<Boolean> = objects.property(Boolean::class.java).convention(true)
        val logSpam: Property<Boolean> = objects.property(Boolean::class.java).convention(true)
        val classGraph: Property<Boolean> = objects.property(Boolean::class.java).convention(true)
        val runtimeCallGraph: Property<Boolean> = objects.property(Boolean::class.java).convention(false)
        val methodCounters: Property<Boolean> = objects.property(Boolean::class.java).convention(false)
        val includeWholeApplication: Property<Boolean> =
            objects.property(Boolean::class.java).convention(false)
        val asmProgressLog: Property<Boolean> = objects.property(Boolean::class.java).convention(false)
        val includePackages: SetProperty<String> = objects.setProperty(String::class.java).convention(emptySet())
        val excludePackages: SetProperty<String> = objects.setProperty(String::class.java).convention(emptySet())

        fun includePackages(vararg values: String) {
            includePackages(values.asIterable())
        }

        fun includePackages(values: Iterable<String>) {
            includePackages.addAll(normalizedPackages(values))
        }

        fun excludePackages(vararg values: String) {
            excludePackages(values.asIterable())
        }

        fun excludePackages(values: Iterable<String>) {
            excludePackages.addAll(normalizedPackages(values))
        }

        private fun normalizedPackages(values: Iterable<String>): Set<String> {
            return values.mapNotNullTo(linkedSetOf()) { it.trim().takeIf(String::isNotEmpty) }
        }
    }

    open class RetainedHeapDump @Inject constructor(objects: ObjectFactory) {
        val enabled: Property<Boolean> = objects.property(Boolean::class.java).convention(false)
        val privacyApproved: Property<Boolean> = objects.property(Boolean::class.java).convention(false)
        val minIntervalMs: Property<Long> = objects.property(Long::class.java).convention(10 * 60_000L)
        val maxCount: Property<Int> = objects.property(Int::class.java).convention(1)
        val minRetainedAgeMs: Property<Long> = objects.property(Long::class.java).convention(30_000L)
    }

    open class Runtime @Inject constructor(objects: ObjectFactory) {
        val mainThreadStallThresholdMs: Property<Long> = objects.property(Long::class.java).convention(700L)
        val ownerBlockThresholdMs: Property<Long> = objects.property(Long::class.java).convention(250L)
        val httpSlowThresholdMs: Property<Long> = objects.property(Long::class.java).convention(1_000L)
        val jankFrameThresholdMs: Property<Long> = objects.property(Long::class.java).convention(32L)
        val uiWindowP95ThresholdMs: Property<Long> = objects.property(Long::class.java).convention(32L)
        val mainLooperDispatchMonitor: Property<Boolean> = objects.property(Boolean::class.java).convention(false)
        val jankStats: Property<Boolean> = objects.property(Boolean::class.java).convention(true)
        val mainProcessOnly: Property<Boolean> = objects.property(Boolean::class.java).convention(true)
    }

    open class ReleaseSafety @Inject constructor(objects: ObjectFactory) {
        val allowInstrumentation: Property<Boolean> = objects.property(Boolean::class.java).convention(false)
        val privacyReviewed: Property<Boolean> = objects.property(Boolean::class.java).convention(false)
        val allowHeapDumps: Property<Boolean> = objects.property(Boolean::class.java).convention(false)
        val allowSecondaryProcesses: Property<Boolean> = objects.property(Boolean::class.java).convention(false)
        val performanceBudgetEvidence: Property<String> = objects.property(String::class.java)
    }
}
