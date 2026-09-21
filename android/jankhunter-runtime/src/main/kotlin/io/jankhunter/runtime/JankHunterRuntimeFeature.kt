package io.jankhunter.runtime

/**
 * Runtime switches for capabilities that may have been added by the Gradle plugin.
 *
 * A switch can disable already instrumented bytecode, but cannot make a capability available when
 * it was not included in the build. Ordinals are persisted as a bit mask and must remain stable.
 */
enum class JankHunterRuntimeFeature {
    JANK_STATS,
    MAIN_LOOPER,
    SQLITE,
    ROOM,
    HTTP,
    WEBSOCKETS,
    RUNTIME_IO,
    BYTECODE_IO,
    DI_ANALYSIS,
    HANDLERS,
    EXECUTORS,
    COROUTINES,
    INTERACTIONS,
    LIFECYCLE_LEAKS,
    LOGGING,
    CLASS_GRAPH,
    CALL_GRAPH,
    COMPOSE,
    WORKERS,
    ANDROID_COMPONENTS,
    BINDER_IPC,
    METHOD_COUNTERS,
    HEAP_DUMPS,
    ;

    internal val mask: Long
        get() = 1L shl ordinal

    internal companion object {
        val allMask: Long = entries.fold(0L) { mask, feature -> mask or feature.mask }

        fun maskOf(features: Iterable<JankHunterRuntimeFeature>): Long {
            var mask = 0L
            features.forEach { feature -> mask = mask or feature.mask }
            return mask
        }

        fun parseAvailable(raw: String?): Set<JankHunterRuntimeFeature> {
            if (raw == null) return entries.toSet()
            if (raw.isBlank()) return emptySet()
            val byName = entries.associateBy(JankHunterRuntimeFeature::name)
            return raw.splitToSequence(',')
                .map(String::trim)
                .mapNotNull(byName::get)
                .toSet()
        }
    }
}
