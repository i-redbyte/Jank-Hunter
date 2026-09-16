package io.jankhunter.runtime

/** One volatile feature decision for instrumentation hot paths. */
internal class RuntimeFeatureGate {
    @Volatile
    private var effectiveMask = 0L

    fun activate(config: JankHunterConfig) {
        effectiveMask = config.effectiveRuntimeFeatureMask()
    }

    fun deactivate() {
        effectiveMask = 0L
    }

    fun isEnabled(feature: JankHunterRuntimeFeature): Boolean {
        return effectiveMask and feature.mask != 0L
    }
}
