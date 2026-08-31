package io.jankhunter.gradle

import org.gradle.api.file.RegularFile
import org.gradle.api.model.ObjectFactory
import org.gradle.api.provider.Property
import java.io.File
import java.util.Locale
import javax.inject.Inject

sealed interface JankHunterStorage {
    data class Limited(val maxSessionMiB: Int) : JankHunterStorage

    data object Unlimited : JankHunterStorage
}

internal enum class JankHunterStorageMode {
    LIMITED,
    UNLIMITED,
}

data class JankHunterVariantIdentity(
    val name: String,
    val buildType: String,
    val productFlavors: Map<String, String> = emptyMap(),
)

data class JankHunterResolvedReleaseSafety(
    val instrumentationApproved: Boolean,
    val privacyReviewed: Boolean,
    val allowHeapDumps: Boolean,
    val allowSecondaryProcesses: Boolean,
    val allowUnlimitedStorage: Boolean,
    val performanceBudgetEvidence: String?,
)

data class JankHunterResolvedRuntime(
    val mainThreadStallThresholdMs: Long,
    val ownerBlockThresholdMs: Long,
    val httpSlowThresholdMs: Long,
    val jankFrameThresholdMs: Long,
    val uiWindowP95ThresholdMs: Long,
    val mainThreadAdmissionWaitMs: Long,
    val backgroundAdmissionWaitMs: Long,
    val mainLooperDispatchMonitor: Boolean,
    val jankStats: Boolean,
    val ioTracing: Boolean,
)

data class JankHunterResolvedInstrumentation(
    val dependencyInjectionAnalysis: Boolean,
    val methodCounters: Boolean,
    val methodFilterMode: JankHunterMethodFilterMode,
    val okhttp: Boolean,
    val webSockets: Boolean,
    val handlers: Boolean,
    val executors: Boolean,
    val coroutines: Boolean,
    val interactionOperations: Boolean,
    val lifecycleLeaks: Boolean,
    val logSpam: Boolean,
    val classGraph: Boolean,
    val runtimeCallGraph: Boolean,
    val composeTracing: Boolean,
    val roomTracing: Boolean,
    val databaseTracing: Boolean,
    val workerTracing: Boolean,
    val androidComponents: Boolean,
    val binderIPC: Boolean,
    val ioTracing: Boolean,
    val scope: JankHunterInstrumentationScope,
) {
    val includeAndroidNamespace: Boolean
        get() = scope.includesAndroidNamespace

    val includeWholeApplication: Boolean
        get() = scope.includesWholeApplication

    val networkWholeApplication: Boolean
        get() = true

    val databaseWholeApplication: Boolean
        get() = true

    val hasRuntimeHooks: Boolean
        get() = methodCounters || okhttp || webSockets || handlers || executors || coroutines ||
            interactionOperations || lifecycleLeaks || logSpam || classGraph || runtimeCallGraph ||
            composeTracing || roomTracing || databaseTracing || workerTracing || ioTracing ||
            androidComponents || binderIPC
}

data class JankHunterResolvedRetainedHeapDump(
    val enabled: Boolean,
    val minIntervalMs: Long,
    val maxCount: Int,
    val minRetainedAgeMs: Long,
)

data class JankHunterResolvedConfiguration(
    val variant: JankHunterVariantIdentity,
    val profile: JankHunterProfile,
    val features: Set<JankHunterFeature>,
    val featureSources: Map<JankHunterFeature, String>,
    val collection: JankHunterCollection,
    val processes: JankHunterProcesses,
    val maxQueueSize: Int,
    val autoInit: Boolean,
    val deleteObsoleteLogs: Boolean,
    val growthAnalytics: Boolean,
    val storage: JankHunterStorage,
    val includePackages: Set<String>,
    val excludePackages: Set<String>,
    val runtime: JankHunterResolvedRuntime,
    val instrumentation: JankHunterResolvedInstrumentation,
    val retainedHeapDump: JankHunterResolvedRetainedHeapDump,
    val releaseSafety: JankHunterResolvedReleaseSafety,
)

open class JankHunterVariantConfiguration @Inject constructor(objects: ObjectFactory) {
    val profile: Property<JankHunterProfile> = objects.property(JankHunterProfile::class.java)
    val collection: Property<JankHunterCollection> = objects.property(JankHunterCollection::class.java)
    val processes: Property<JankHunterProcesses> = objects.property(JankHunterProcesses::class.java)
    val scope: Property<JankHunterInstrumentationScope> = objects.property(JankHunterInstrumentationScope::class.java)
    val tuning: JankHunterTuning = objects.newInstance(JankHunterTuning::class.java)
    val growthAnalytics: Property<Boolean> = objects.property(Boolean::class.java)
    val autoInit: Property<Boolean> = objects.property(Boolean::class.java)
    val deleteObsoleteLogs: Property<Boolean> = objects.property(Boolean::class.java)

    internal val storageMode: Property<JankHunterStorageMode> = objects.property(JankHunterStorageMode::class.java)
    internal val storageLimitMiB: Property<Int> = objects.property(Int::class.java)
    internal val instrumentationApproved: Property<Boolean> = objects.property(Boolean::class.java)
    internal val privacyReviewedApproval: Property<Boolean> = objects.property(Boolean::class.java)
    internal val heapDumpsApproval: Property<Boolean> = objects.property(Boolean::class.java)
    internal val secondaryProcessesApproval: Property<Boolean> = objects.property(Boolean::class.java)
    internal val unlimitedStorageApproval: Property<Boolean> = objects.property(Boolean::class.java)
    internal val performanceBudgetEvidence: Property<String> = objects.property(String::class.java)

    private val enabledFeatures = linkedSetOf<JankHunterFeatureSelection>()
    private val disabledFeatures = linkedSetOf<JankHunterFeatureSelection>()
    private val includedPackages = linkedSetOf<String>()
    private val excludedPackages = linkedSetOf<String>()

    fun enable(vararg selections: JankHunterFeatureSelection) {
        enabledFeatures.addAll(selections)
    }

    fun disable(vararg selections: JankHunterFeatureSelection) {
        disabledFeatures.addAll(selections)
    }

    fun packages(vararg values: String) {
        addPackages(includedPackages, values.asIterable())
    }

    fun excludePackages(vararg values: String) {
        addPackages(excludedPackages, values.asIterable())
    }

    fun storageLimitMiB(value: Int) {
        storageMode.set(JankHunterStorageMode.LIMITED)
        storageLimitMiB.set(value)
    }

    fun unlimitedStorage() {
        storageMode.set(JankHunterStorageMode.UNLIMITED)
    }

    fun privacyReviewed() {
        privacyReviewedApproval.set(true)
    }

    fun allowHeapDumps() {
        heapDumpsApproval.set(true)
    }

    fun allowSecondaryProcesses() {
        secondaryProcessesApproval.set(true)
    }

    fun allowUnlimitedStorage() {
        unlimitedStorageApproval.set(true)
    }

    fun performanceBudget(file: File) {
        performanceBudgetEvidence.set(file.absolutePath)
    }

    fun performanceBudget(file: RegularFile) {
        performanceBudget(file.asFile)
    }

    internal fun featureLayer(
        priority: JankHunterConfigurationPriority,
        source: String,
    ): JankHunterConfigurationLayer {
        return JankHunterConfigurationLayer(
            priority = priority,
            source = source,
            profile = profile.orNull,
            enabled = enabledFeatures,
            disabled = disabledFeatures,
        )
    }

    internal fun includedPackages(): Set<String> = includedPackages

    internal fun excludedPackages(): Set<String> = excludedPackages

    internal fun hasFunctionalOverrides(): Boolean {
        return profile.isPresent ||
            collection.isPresent ||
            processes.isPresent ||
            scope.isPresent ||
            tuning.hasOverrides() ||
            growthAnalytics.isPresent ||
            autoInit.isPresent ||
            deleteObsoleteLogs.isPresent ||
            storageMode.isPresent ||
            storageLimitMiB.isPresent ||
            enabledFeatures.isNotEmpty() ||
            disabledFeatures.isNotEmpty() ||
            includedPackages.isNotEmpty() ||
            excludedPackages.isNotEmpty()
    }

    private fun addPackages(destination: MutableSet<String>, values: Iterable<String>) {
        values.forEach { raw ->
            raw.trim().takeIf(String::isNotEmpty)?.let(destination::add)
        }
    }
}

internal class JankHunterVariantRule(
    val source: String,
    val priority: JankHunterConfigurationPriority,
    val configuration: JankHunterVariantConfiguration,
)

internal object JankHunterVariantConfigurationResolver {
    fun resolve(
        identity: JankHunterVariantIdentity,
        layers: List<JankHunterVariantRule>,
    ): JankHunterResolvedConfiguration {
        val features = JankHunterConfigurationResolver.resolveFeatures(
            layers.map { rule -> rule.configuration.featureLayer(rule.priority, rule.source) },
        )
        val profile = features.profile.definition
        val collection = resolveScalar("collection", layers, profile.collection) { it.collection.orNull }
        val processes = resolveScalar("processes", layers, profile.processes) { it.processes.orNull }
        val maxQueueSize = resolveScalar("tuning.queueCapacity", layers, profile.queueCapacity) {
            it.tuning.queueCapacity.orNull
        }
        val growthAnalytics = resolveScalar("growthAnalytics", layers, true) { it.growthAnalytics.orNull }
        val privacyReviewed = resolveScalar("privacyReviewed", layers, false) {
            it.privacyReviewedApproval.orNull
        }
        val storage = resolveStorage(layers)
        val includePackages = collectPackages(layers, JankHunterVariantConfiguration::includedPackages)
        val excludePackages = collectPackages(layers, JankHunterVariantConfiguration::excludedPackages)
        validatePositive("maxQueueSize", maxQueueSize)

        return JankHunterResolvedConfiguration(
            variant = identity,
            profile = features.profile,
            features = features.enabled,
            featureSources = features.sources,
            collection = collection,
            processes = processes,
            maxQueueSize = maxQueueSize,
            autoInit = boolean("autoInit", layers, true, JankHunterVariantConfiguration::autoInit),
            deleteObsoleteLogs = boolean(
                "deleteObsoleteLogs",
                layers,
                false,
                JankHunterVariantConfiguration::deleteObsoleteLogs,
            ),
            growthAnalytics = growthAnalytics,
            storage = storage,
            includePackages = includePackages,
            excludePackages = excludePackages,
            runtime = resolveRuntime(layers, features.enabled),
            instrumentation = resolveInstrumentation(layers, features.enabled),
            retainedHeapDump = resolveRetainedHeapDump(layers, features.enabled),
            releaseSafety = JankHunterResolvedReleaseSafety(
                instrumentationApproved = resolveScalar(
                    "instrumentationApproved",
                    layers,
                    false,
                ) { it.instrumentationApproved.orNull },
                privacyReviewed = privacyReviewed,
                allowHeapDumps = resolveScalar("allowHeapDumps", layers, false) {
                    it.heapDumpsApproval.orNull
                },
                allowSecondaryProcesses = resolveScalar("allowSecondaryProcesses", layers, false) {
                    it.secondaryProcessesApproval.orNull
                },
                allowUnlimitedStorage = resolveScalar("allowUnlimitedStorage", layers, false) {
                    it.unlimitedStorageApproval.orNull
                },
                performanceBudgetEvidence = resolveNullableScalar("performanceBudget", layers) {
                    it.performanceBudgetEvidence.orNull
                },
            ),
        )
    }

    private fun resolveRuntime(
        layers: List<JankHunterVariantRule>,
        features: Set<JankHunterFeature>,
    ): JankHunterResolvedRuntime {
        return JankHunterResolvedRuntime(
            mainThreadStallThresholdMs = long(
                "tuning.thresholds.mainThreadStallMs",
                layers,
                700L,
            ) { it.tuning.thresholds.mainThreadStallMs },
            ownerBlockThresholdMs = long(
                "tuning.thresholds.ownerBlockMs",
                layers,
                250L,
            ) { it.tuning.thresholds.ownerBlockMs },
            httpSlowThresholdMs = long(
                "tuning.thresholds.slowHttpMs",
                layers,
                1_000L,
            ) { it.tuning.thresholds.slowHttpMs },
            jankFrameThresholdMs = long(
                "tuning.thresholds.jankFrameMs",
                layers,
                32L,
            ) { it.tuning.thresholds.jankFrameMs },
            uiWindowP95ThresholdMs = long(
                "tuning.thresholds.uiWindowP95Ms",
                layers,
                32L,
            ) { it.tuning.thresholds.uiWindowP95Ms },
            mainThreadAdmissionWaitMs = long(
                "tuning.admission.mainThreadWaitMs",
                layers,
                0L,
            ) { it.tuning.admission.mainThreadWaitMs },
            backgroundAdmissionWaitMs = long(
                "tuning.admission.backgroundWaitMs",
                layers,
                5L,
            ) { it.tuning.admission.backgroundWaitMs },
            mainLooperDispatchMonitor = JankHunterFeature.MAIN_LOOPER in features,
            jankStats = JankHunterFeature.JANK_STATS in features,
            ioTracing = JankHunterFeature.RUNTIME_IO in features,
        )
    }

    private fun resolveInstrumentation(
        layers: List<JankHunterVariantRule>,
        features: Set<JankHunterFeature>,
    ): JankHunterResolvedInstrumentation {
        return JankHunterResolvedInstrumentation(
            dependencyInjectionAnalysis = JankHunterFeature.DI_ANALYSIS in features,
            methodCounters = JankHunterFeature.METHOD_COUNTERS in features,
            methodFilterMode = resolveScalar(
                "tuning.methodFiltering",
                layers,
                JankHunterMethodFilterMode.FILTER,
            ) { it.tuning.methodFiltering.orNull },
            okhttp = JankHunterFeature.HTTP in features,
            webSockets = JankHunterFeature.WEBSOCKETS in features,
            handlers = JankHunterFeature.HANDLERS in features,
            executors = JankHunterFeature.EXECUTORS in features,
            coroutines = JankHunterFeature.COROUTINES in features,
            interactionOperations = JankHunterFeature.INTERACTIONS in features,
            lifecycleLeaks = JankHunterFeature.LIFECYCLE_LEAKS in features,
            logSpam = JankHunterFeature.LOGGING in features,
            classGraph = JankHunterFeature.CLASS_GRAPH in features,
            runtimeCallGraph = JankHunterFeature.CALL_GRAPH in features,
            composeTracing = JankHunterFeature.COMPOSE in features,
            roomTracing = JankHunterFeature.ROOM in features,
            databaseTracing = JankHunterFeature.SQLITE in features,
            workerTracing = JankHunterFeature.WORKERS in features,
            androidComponents = JankHunterFeature.ANDROID_COMPONENTS in features,
            binderIPC = JankHunterFeature.BINDER_IPC in features,
            ioTracing = JankHunterFeature.BYTECODE_IO in features,
            scope = resolveScalar(
                "scope",
                layers,
                JankHunterInstrumentationScope.NAMESPACE_AND_PACKAGES,
            ) { it.scope.orNull },
        )
    }

    private fun resolveRetainedHeapDump(
        layers: List<JankHunterVariantRule>,
        features: Set<JankHunterFeature>,
    ): JankHunterResolvedRetainedHeapDump {
        val maxCount = resolveScalar("tuning.heapDumps.maxCount", layers, 1) {
            it.tuning.heapDumps.maxCount.orNull
        }
        validatePositive("tuning.heapDumps.maxCount", maxCount)
        return JankHunterResolvedRetainedHeapDump(
            enabled = JankHunterFeature.HEAP_DUMPS in features,
            minIntervalMs = long(
                "tuning.heapDumps.minIntervalMs",
                layers,
                10 * 60_000L,
            ) { it.tuning.heapDumps.minIntervalMs },
            maxCount = maxCount,
            minRetainedAgeMs = long(
                "tuning.heapDumps.minRetainedAgeMs",
                layers,
                30_000L,
            ) { it.tuning.heapDumps.minRetainedAgeMs },
        )
    }

    private fun boolean(
        name: String,
        layers: List<JankHunterVariantRule>,
        default: Boolean,
        selector: (JankHunterVariantConfiguration) -> Property<Boolean>,
    ): Boolean = resolveScalar(name, layers, default) { selector(it).orNull }

    private fun long(
        name: String,
        layers: List<JankHunterVariantRule>,
        default: Long,
        selector: (JankHunterVariantConfiguration) -> Property<Long>,
    ): Long = resolveScalar(name, layers, default) { selector(it).orNull }

    private fun resolveStorage(layers: List<JankHunterVariantRule>): JankHunterStorage {
        val mode = resolveScalar("storage", layers, JankHunterStorageMode.LIMITED) { it.storageMode.orNull }
        if (mode == JankHunterStorageMode.UNLIMITED) return JankHunterStorage.Unlimited
        val limit = resolveScalar("storageLimitMiB", layers, DEFAULT_STORAGE_LIMIT_MIB) {
            it.storageLimitMiB.orNull
        }
        validatePositive("storageLimitMiB", limit)
        return JankHunterStorage.Limited(limit)
    }

    private fun collectPackages(
        layers: List<JankHunterVariantRule>,
        selector: (JankHunterVariantConfiguration) -> Set<String>,
    ): Set<String> {
        val result = linkedSetOf<String>()
        layers.sortedBy(JankHunterVariantRule::priority).forEach { rule ->
            result.addAll(selector(rule.configuration))
        }
        return result
    }

    private fun <T : Any> resolveScalar(
        name: String,
        layers: List<JankHunterVariantRule>,
        default: T,
        selector: (JankHunterVariantConfiguration) -> T?,
    ): T {
        return resolveNullableScalar(name, layers, selector) ?: default
    }

    private fun <T : Any> resolveNullableScalar(
        name: String,
        layers: List<JankHunterVariantRule>,
        selector: (JankHunterVariantConfiguration) -> T?,
    ): T? {
        var resolved: T? = null
        layers.groupBy(JankHunterVariantRule::priority)
            .toSortedMap(compareBy(JankHunterConfigurationPriority::order))
            .forEach { (priority, samePriorityLayers) ->
                val candidates = samePriorityLayers.mapNotNull { rule ->
                    selector(rule.configuration)?.let { value -> ScalarCandidate(value, rule.source) }
                }
                if (candidates.isNotEmpty()) {
                    val values = candidates.map(ScalarCandidate<T>::value).distinct()
                    if (values.size > 1) throw scalarConflict(name, priority, candidates)
                    resolved = values.single()
                }
            }
        return resolved
    }

    private fun <T : Any> scalarConflict(
        name: String,
        priority: JankHunterConfigurationPriority,
        candidates: List<ScalarCandidate<T>>,
    ): JankHunterConfigurationException {
        return JankHunterConfigurationException(
            buildString {
                append("Jank Hunter configuration error at ")
                append(priority.name)
                append(". Property '")
                append(name)
                append("' has conflicting values:")
                candidates.forEach { candidate ->
                    append("\n- ")
                    append(candidate.source)
                    append(" = ")
                    append(candidate.value)
                }
                append("\nFix: set one value in an exact variant override.")
            },
        )
    }

    private fun validatePositive(name: String, value: Int) {
        if (value <= 0) {
            throw JankHunterConfigurationException(
                "Jank Hunter configuration error. '$name' must be greater than zero, but was $value.",
            )
        }
    }

    private data class ScalarCandidate<T : Any>(val value: T, val source: String)

    private const val DEFAULT_STORAGE_LIMIT_MIB = 50
}

internal fun normalizedBuildType(value: String): String = value.trim().lowercase(Locale.US)
