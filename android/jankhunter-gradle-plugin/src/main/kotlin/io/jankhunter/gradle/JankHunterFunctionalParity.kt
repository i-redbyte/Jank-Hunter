package io.jankhunter.gradle

import java.util.Locale

internal object JankHunterFunctionalParity {
    fun differences(
        debug: JankHunterResolvedConfiguration,
        release: JankHunterResolvedConfiguration,
    ): List<String> {
        val differences = ArrayList<String>(8)
        compare(differences, "collection", debug.collection, release.collection)
        compare(differences, "processes", debug.processes, release.processes)
        compare(differences, "tuning.queueCapacity", debug.maxQueueSize, release.maxQueueSize)
        compare(differences, "autoInit", debug.autoInit, release.autoInit)
        compare(differences, "deleteObsoleteLogs", debug.deleteObsoleteLogs, release.deleteObsoleteLogs)
        compare(differences, "growthAnalytics", debug.growthAnalytics, release.growthAnalytics)
        compare(differences, "storage", debug.storage, release.storage)
        compare(
            differences,
            "packages.include",
            debug.includePackages.sorted(),
            release.includePackages.sorted(),
        )
        compare(
            differences,
            "packages.exclude",
            debug.excludePackages.sorted(),
            release.excludePackages.sorted(),
        )
        JankHunterFeature.entries.forEach { feature ->
            compare(
                differences,
                "features.${feature.name.lowercase(Locale.US)}",
                feature in debug.features,
                feature in release.features,
            )
        }
        compareRuntime(differences, debug.runtime, release.runtime)
        compareInstrumentation(differences, debug.instrumentation, release.instrumentation)
        compareHeapDumpTuning(differences, debug.retainedHeapDump, release.retainedHeapDump)
        return differences
    }

    private fun compareRuntime(
        differences: MutableList<String>,
        debug: JankHunterResolvedRuntime,
        release: JankHunterResolvedRuntime,
    ) {
        compare(
            differences,
            "tuning.thresholds.mainThreadStallMs",
            debug.mainThreadStallThresholdMs,
            release.mainThreadStallThresholdMs,
        )
        compare(
            differences,
            "tuning.thresholds.ownerBlockMs",
            debug.ownerBlockThresholdMs,
            release.ownerBlockThresholdMs,
        )
        compare(
            differences,
            "tuning.thresholds.slowHttpMs",
            debug.httpSlowThresholdMs,
            release.httpSlowThresholdMs,
        )
        compare(
            differences,
            "tuning.thresholds.jankFrameMs",
            debug.jankFrameThresholdMs,
            release.jankFrameThresholdMs,
        )
        compare(
            differences,
            "tuning.thresholds.uiWindowP95Ms",
            debug.uiWindowP95ThresholdMs,
            release.uiWindowP95ThresholdMs,
        )
        compare(
            differences,
            "tuning.admission.mainThreadWaitMs",
            debug.mainThreadAdmissionWaitMs,
            release.mainThreadAdmissionWaitMs,
        )
        compare(
            differences,
            "tuning.admission.backgroundWaitMs",
            debug.backgroundAdmissionWaitMs,
            release.backgroundAdmissionWaitMs,
        )
    }

    private fun compareInstrumentation(
        differences: MutableList<String>,
        debug: JankHunterResolvedInstrumentation,
        release: JankHunterResolvedInstrumentation,
    ) {
        compare(differences, "scope", debug.scope, release.scope)
        compare(
            differences,
            "tuning.methodFiltering",
            debug.methodFilterMode,
            release.methodFilterMode,
        )
    }

    private fun compareHeapDumpTuning(
        differences: MutableList<String>,
        debug: JankHunterResolvedRetainedHeapDump,
        release: JankHunterResolvedRetainedHeapDump,
    ) {
        compare(
            differences,
            "tuning.heapDumps.minIntervalMs",
            debug.minIntervalMs,
            release.minIntervalMs,
        )
        compare(differences, "tuning.heapDumps.maxCount", debug.maxCount, release.maxCount)
        compare(
            differences,
            "tuning.heapDumps.minRetainedAgeMs",
            debug.minRetainedAgeMs,
            release.minRetainedAgeMs,
        )
    }

    private fun compare(
        differences: MutableList<String>,
        path: String,
        debug: Any,
        release: Any,
    ) {
        if (debug != release) differences += "$path: debug=$debug, release=$release"
    }
}
