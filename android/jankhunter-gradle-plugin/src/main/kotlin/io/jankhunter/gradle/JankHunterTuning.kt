package io.jankhunter.gradle

import org.gradle.api.model.ObjectFactory
import org.gradle.api.provider.Property
import javax.inject.Inject

enum class JankHunterInstrumentationScope {
    NAMESPACE_AND_PACKAGES,
    PACKAGES_ONLY,
    WHOLE_APPLICATION,
    ;

    internal val includesAndroidNamespace: Boolean
        get() = this == NAMESPACE_AND_PACKAGES

    internal val includesWholeApplication: Boolean
        get() = this == WHOLE_APPLICATION
}

open class JankHunterTuning @Inject constructor(objects: ObjectFactory) {
    val queueCapacity: Property<Int> = objects.property(Int::class.java)
    val methodFiltering: Property<JankHunterMethodFilterMode> =
        objects.property(JankHunterMethodFilterMode::class.java)
    val thresholds: JankHunterThresholdTuning = objects.newInstance(JankHunterThresholdTuning::class.java)
    val admission: JankHunterAdmissionTuning = objects.newInstance(JankHunterAdmissionTuning::class.java)
    val heapDumps: JankHunterHeapDumpTuning = objects.newInstance(JankHunterHeapDumpTuning::class.java)

    internal fun hasOverrides(): Boolean {
        return queueCapacity.isPresent ||
            methodFiltering.isPresent ||
            thresholds.hasOverrides() ||
            admission.hasOverrides() ||
            heapDumps.hasOverrides()
    }
}

open class JankHunterThresholdTuning @Inject constructor(objects: ObjectFactory) {
    val mainThreadStallMs: Property<Long> = objects.property(Long::class.java)
    val ownerBlockMs: Property<Long> = objects.property(Long::class.java)
    val slowHttpMs: Property<Long> = objects.property(Long::class.java)
    val jankFrameMs: Property<Long> = objects.property(Long::class.java)
    val uiWindowP95Ms: Property<Long> = objects.property(Long::class.java)

    internal fun hasOverrides(): Boolean {
        return mainThreadStallMs.isPresent || ownerBlockMs.isPresent || slowHttpMs.isPresent ||
            jankFrameMs.isPresent || uiWindowP95Ms.isPresent
    }
}

open class JankHunterAdmissionTuning @Inject constructor(objects: ObjectFactory) {
    val mainThreadWaitMs: Property<Long> = objects.property(Long::class.java)
    val backgroundWaitMs: Property<Long> = objects.property(Long::class.java)

    internal fun hasOverrides(): Boolean = mainThreadWaitMs.isPresent || backgroundWaitMs.isPresent
}

open class JankHunterHeapDumpTuning @Inject constructor(objects: ObjectFactory) {
    val minIntervalMs: Property<Long> = objects.property(Long::class.java)
    val maxCount: Property<Int> = objects.property(Int::class.java)
    val minRetainedAgeMs: Property<Long> = objects.property(Long::class.java)

    internal fun hasOverrides(): Boolean {
        return minIntervalMs.isPresent || maxCount.isPresent || minRetainedAgeMs.isPresent
    }
}
