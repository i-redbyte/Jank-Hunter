package io.jankhunter.gradle

import java.util.EnumMap
import java.util.EnumSet

sealed interface JankHunterFeatureSelection

enum class JankHunterFeature : JankHunterFeatureSelection {
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
}

enum class JankHunterFeatureBundle(
    internal val features: Set<JankHunterFeature>,
) : JankHunterFeatureSelection {
    UI_ALL(enumSetOf(JankHunterFeature.JANK_STATS, JankHunterFeature.MAIN_LOOPER)),
    NETWORK_ALL(enumSetOf(JankHunterFeature.HTTP, JankHunterFeature.WEBSOCKETS)),
    DATABASE_ALL(enumSetOf(JankHunterFeature.SQLITE, JankHunterFeature.ROOM)),
    IO_ALL(enumSetOf(JankHunterFeature.RUNTIME_IO, JankHunterFeature.BYTECODE_IO)),
    CONCURRENCY_ALL(
        enumSetOf(
            JankHunterFeature.HANDLERS,
            JankHunterFeature.EXECUTORS,
            JankHunterFeature.COROUTINES,
        ),
    ),
    ANDROID_SYSTEM_ALL(
        enumSetOf(
            JankHunterFeature.ANDROID_COMPONENTS,
            JankHunterFeature.BINDER_IPC,
        ),
    ),
}

enum class JankHunterCollection {
    BALANCED,
    EXACT,
}

enum class JankHunterProcesses {
    MAIN_ONLY,
    ALL,
}

enum class JankHunterProfile {
    MINIMAL,
    BALANCED,
    FULL,
    RELEASE_SAFE,
    TARGETED,
    ;

    internal val definition: JankHunterProfileDefinition
        get() = PROFILE_DEFINITIONS.getValue(this)
}

internal data class JankHunterProfileDefinition(
    val description: String,
    val collection: JankHunterCollection,
    val queueCapacity: Int,
    val processes: JankHunterProcesses,
    val features: Set<JankHunterFeature>,
)

internal enum class JankHunterConfigurationPriority(val order: Int) {
    GLOBAL(0),
    BUILD_TYPE(1),
    FLAVOR(2),
    EXACT_VARIANT(3),
}

internal data class JankHunterConfigurationLayer(
    val priority: JankHunterConfigurationPriority,
    val source: String,
    val profile: JankHunterProfile? = null,
    val enabled: Set<JankHunterFeatureSelection> = emptySet(),
    val disabled: Set<JankHunterFeatureSelection> = emptySet(),
)

internal data class JankHunterResolvedFeatures(
    val profile: JankHunterProfile,
    val enabled: Set<JankHunterFeature>,
    val sources: Map<JankHunterFeature, String>,
)

internal class JankHunterConfigurationException(message: String) : IllegalArgumentException(message)

internal object JankHunterConfigurationResolver {
    fun resolveFeatures(layers: List<JankHunterConfigurationLayer>): JankHunterResolvedFeatures {
        val profile = resolveProfile(layers)
        val resolved = enumSetOf(*profile.definition.features.toTypedArray())
        val sources = EnumMap<JankHunterFeature, String>(JankHunterFeature::class.java)
        profile.definition.features.forEach { feature -> sources[feature] = "profile(${profile.name})" }

        layers.groupBy(JankHunterConfigurationLayer::priority)
            .toSortedMap(compareBy(JankHunterConfigurationPriority::order))
            .forEach { (priority, samePriorityLayers) ->
                applyFeatureDirectives(priority, samePriorityLayers, resolved, sources)
            }

        return JankHunterResolvedFeatures(
            profile = profile,
            enabled = resolved.toSet(),
            sources = sources.toMap(),
        )
    }

    private fun resolveProfile(layers: List<JankHunterConfigurationLayer>): JankHunterProfile {
        val configured = layers.filter { layer -> layer.profile != null }
        if (configured.isEmpty()) return JankHunterProfile.BALANCED
        val priority = configured.maxOf { layer -> layer.priority.order }
        val mostSpecific = configured.filter { layer -> layer.priority.order == priority }
        val profiles = mostSpecific.mapNotNull(JankHunterConfigurationLayer::profile).distinct()
        if (profiles.size == 1) return profiles.single()
        throw JankHunterConfigurationException(
            buildString {
                append("Jank Hunter configuration error. Conflicting profiles at priority ")
                append(mostSpecific.first().priority.name)
                append(':')
                mostSpecific.forEach { layer ->
                    append("\n- ")
                    append(layer.source)
                    append(" selected ")
                    append(layer.profile)
                }
                append("\nFix: select one profile in an exact variant override.")
            },
        )
    }

    private fun applyFeatureDirectives(
        priority: JankHunterConfigurationPriority,
        layers: List<JankHunterConfigurationLayer>,
        resolved: EnumSet<JankHunterFeature>,
        sources: EnumMap<JankHunterFeature, String>,
    ) {
        val directives = EnumMap<JankHunterFeature, MutableList<FeatureDirective>>(JankHunterFeature::class.java)
        layers.forEach { layer ->
            collectDirectives(directives, layer.enabled, enabled = true, layer.source)
            collectDirectives(directives, layer.disabled, enabled = false, layer.source)
        }
        directives.forEach { (feature, candidates) ->
            val specificity = candidates.maxOf(FeatureDirective::specificity)
            val effective = candidates.filter { directive -> directive.specificity == specificity }
            val enabled = effective.first().enabled
            if (effective.any { directive -> directive.enabled != enabled }) {
                throw featureConflict(priority, feature, effective)
            }
            if (enabled) resolved.add(feature) else resolved.remove(feature)
            sources[feature] = effective.joinToString { directive -> directive.source }
        }
    }

    private fun collectDirectives(
        directives: EnumMap<JankHunterFeature, MutableList<FeatureDirective>>,
        selections: Set<JankHunterFeatureSelection>,
        enabled: Boolean,
        source: String,
    ) {
        selections.forEach { selection ->
            when (selection) {
                is JankHunterFeature -> directives.add(selection, FeatureDirective(enabled, ATOMIC, source))
                is JankHunterFeatureBundle -> selection.features.forEach { feature ->
                    directives.add(feature, FeatureDirective(enabled, BUNDLE, source))
                }
            }
        }
    }

    private fun featureConflict(
        priority: JankHunterConfigurationPriority,
        feature: JankHunterFeature,
        directives: List<FeatureDirective>,
    ): JankHunterConfigurationException {
        val enabledBy = directives.filter(FeatureDirective::enabled).joinToString { directive -> directive.source }
        val disabledBy = directives.filterNot(FeatureDirective::enabled).joinToString { directive -> directive.source }
        return JankHunterConfigurationException(
            "Jank Hunter configuration error at ${priority.name}. " +
                "${feature.name} was explicitly enabled and disabled at the same priority level.\n" +
                "Enabled by: $enabledBy\nDisabled by: $disabledBy\n" +
                "Fix: resolve the conflict in an exact variant override.",
        )
    }

    private fun EnumMap<JankHunterFeature, MutableList<FeatureDirective>>.add(
        feature: JankHunterFeature,
        directive: FeatureDirective,
    ) {
        getOrPut(feature, ::ArrayList).add(directive)
    }

    private data class FeatureDirective(
        val enabled: Boolean,
        val specificity: Int,
        val source: String,
    )

    private const val BUNDLE = 0
    private const val ATOMIC = 1
}

private val PROFILE_DEFINITIONS: Map<JankHunterProfile, JankHunterProfileDefinition> = mapOf(
    JankHunterProfile.MINIMAL to JankHunterProfileDefinition(
        description = "Low-overhead runtime telemetry.",
        collection = JankHunterCollection.BALANCED,
        queueCapacity = 8_192,
        processes = JankHunterProcesses.MAIN_ONLY,
        features = enumSetOf(JankHunterFeature.JANK_STATS),
    ),
    JankHunterProfile.BALANCED to JankHunterProfileDefinition(
        description = "Standard development and QA diagnostics.",
        collection = JankHunterCollection.BALANCED,
        queueCapacity = 32_768,
        processes = JankHunterProcesses.MAIN_ONLY,
        features = enumSetOf(
            JankHunterFeature.JANK_STATS,
            JankHunterFeature.SQLITE,
            JankHunterFeature.ROOM,
            JankHunterFeature.HTTP,
            JankHunterFeature.HANDLERS,
            JankHunterFeature.EXECUTORS,
            JankHunterFeature.COROUTINES,
            JankHunterFeature.INTERACTIONS,
            JankHunterFeature.LIFECYCLE_LEAKS,
            JankHunterFeature.LOGGING,
            JankHunterFeature.CLASS_GRAPH,
            JankHunterFeature.COMPOSE,
            JankHunterFeature.WORKERS,
            JankHunterFeature.ANDROID_COMPONENTS,
        ),
    ),
    JankHunterProfile.FULL to JankHunterProfileDefinition(
        description = "All safe diagnostics with exact collection.",
        collection = JankHunterCollection.EXACT,
        queueCapacity = 65_536,
        processes = JankHunterProcesses.ALL,
        features = enumSetOf(
            JankHunterFeature.JANK_STATS,
            JankHunterFeature.SQLITE,
            JankHunterFeature.ROOM,
            JankHunterFeature.HTTP,
            JankHunterFeature.WEBSOCKETS,
            JankHunterFeature.RUNTIME_IO,
            JankHunterFeature.BYTECODE_IO,
            JankHunterFeature.DI_ANALYSIS,
            JankHunterFeature.HANDLERS,
            JankHunterFeature.EXECUTORS,
            JankHunterFeature.COROUTINES,
            JankHunterFeature.INTERACTIONS,
            JankHunterFeature.LIFECYCLE_LEAKS,
            JankHunterFeature.LOGGING,
            JankHunterFeature.CLASS_GRAPH,
            JankHunterFeature.CALL_GRAPH,
            JankHunterFeature.COMPOSE,
            JankHunterFeature.WORKERS,
            JankHunterFeature.ANDROID_COMPONENTS,
            JankHunterFeature.BINDER_IPC,
        ),
    ),
    JankHunterProfile.RELEASE_SAFE to JankHunterProfileDefinition(
        description = "Bounded diagnostics intended for release builds.",
        collection = JankHunterCollection.BALANCED,
        queueCapacity = 16_384,
        processes = JankHunterProcesses.MAIN_ONLY,
        features = enumSetOf(
            JankHunterFeature.JANK_STATS,
            JankHunterFeature.SQLITE,
            JankHunterFeature.ROOM,
            JankHunterFeature.HTTP,
            JankHunterFeature.INTERACTIONS,
            JankHunterFeature.LIFECYCLE_LEAKS,
            JankHunterFeature.COMPOSE,
            JankHunterFeature.WORKERS,
            JankHunterFeature.ANDROID_COMPONENTS,
        ),
    ),
    JankHunterProfile.TARGETED to JankHunterProfileDefinition(
        description = "Exact collection for explicitly selected diagnostic domains.",
        collection = JankHunterCollection.EXACT,
        queueCapacity = 65_536,
        processes = JankHunterProcesses.MAIN_ONLY,
        features = emptySet(),
    ),
)

private fun enumSetOf(vararg values: JankHunterFeature): EnumSet<JankHunterFeature> {
    val result = EnumSet.noneOf(JankHunterFeature::class.java)
    values.forEach(result::add)
    return result
}
