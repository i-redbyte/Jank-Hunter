package io.jankhunter.gradle

import org.gradle.api.model.ObjectFactory
import org.gradle.api.file.RegularFile
import org.gradle.api.provider.Property
import org.gradle.api.provider.SetProperty
import java.io.File
import javax.inject.Inject

class JankHunterReleaseApproval internal constructor(
    private val configuration: JankHunterVariantConfiguration,
) {
    fun privacyReviewed() {
        configuration.privacyReviewed()
    }

    fun allowHeapDumps() {
        configuration.allowHeapDumps()
    }

    fun allowSecondaryProcesses() {
        configuration.allowSecondaryProcesses()
    }

    fun allowUnlimitedStorage() {
        configuration.allowUnlimitedStorage()
    }

    fun performanceBudget(file: File) {
        configuration.performanceBudget(file)
    }

    fun performanceBudget(file: RegularFile) {
        configuration.performanceBudget(file)
    }
}

open class JankHunterExtension @Inject constructor(private val objects: ObjectFactory) {
    private val conciseDefaults: JankHunterVariantConfiguration =
        objects.newInstance(JankHunterVariantConfiguration::class.java)
    private val buildTypeConfigurations = linkedMapOf<String, JankHunterVariantConfiguration>()
    private val flavorConfigurations = linkedMapOf<Pair<String, String>, JankHunterVariantConfiguration>()
    private val variantConfigurations = linkedMapOf<String, JankHunterVariantConfiguration>()

    val enabled: Property<Boolean> = objects.property(Boolean::class.java).convention(true)
    val enabledBuildTypes: SetProperty<String> = objects.setProperty(String::class.java).convention(setOf("debug"))
    val profile: Property<JankHunterProfile> = conciseDefaults.profile
    val collection: Property<JankHunterCollection> = conciseDefaults.collection
    val processes: Property<JankHunterProcesses> = conciseDefaults.processes
    val scope: Property<JankHunterInstrumentationScope> = conciseDefaults.scope
    val tuning: JankHunterTuning = conciseDefaults.tuning
    val growthAnalytics: Property<Boolean> = conciseDefaults.growthAnalytics
    val autoInit: Property<Boolean> = conciseDefaults.autoInit
    val deleteObsoleteLogs: Property<Boolean> = conciseDefaults.deleteObsoleteLogs

    init {
        profile.convention(JankHunterProfile.BALANCED)
        scope.convention(JankHunterInstrumentationScope.NAMESPACE_AND_PACKAGES)
        growthAnalytics.convention(true)
        autoInit.convention(true)
        deleteObsoleteLogs.convention(false)
    }

    fun enable(vararg selections: JankHunterFeatureSelection) {
        conciseDefaults.enable(*selections)
    }

    fun disable(vararg selections: JankHunterFeatureSelection) {
        conciseDefaults.disable(*selections)
    }

    fun packages(vararg values: String) {
        conciseDefaults.packages(*values)
    }

    fun excludePackages(vararg values: String) {
        conciseDefaults.excludePackages(*values)
    }

    fun storageLimitMiB(value: Int) {
        conciseDefaults.storageLimitMiB(value)
    }

    fun unlimitedStorage() {
        conciseDefaults.unlimitedStorage()
    }

    fun privacyReviewed() {
        conciseDefaults.privacyReviewed()
    }

    fun debug(action: JankHunterVariantConfiguration.() -> Unit) {
        buildTypeConfiguration("debug").action()
    }

    fun release(action: JankHunterReleaseApproval.() -> Unit) {
        val configuration = buildTypeConfiguration("release")
        configuration.instrumentationApproved.set(true)
        JankHunterReleaseApproval(configuration).action()
    }

    fun buildType(
        name: String,
        profile: JankHunterProfile,
        action: JankHunterVariantConfiguration.() -> Unit = {},
    ) {
        val normalized = normalizedBuildType(name)
        require(normalized.isNotEmpty()) { "Jank Hunter build type name must not be blank." }
        val configuration = buildTypeConfiguration(normalized)
        configuration.profile.set(profile)
        configuration.action()
    }

    fun flavor(
        dimension: String,
        name: String,
        action: JankHunterVariantConfiguration.() -> Unit,
    ) {
        val normalizedDimension = dimension.trim()
        val normalizedName = name.trim()
        require(normalizedDimension.isNotEmpty()) { "Jank Hunter flavor dimension must not be blank." }
        require(normalizedName.isNotEmpty()) { "Jank Hunter flavor name must not be blank." }
        val configuration = flavorConfigurations.getOrPut(normalizedDimension to normalizedName) {
            objects.newInstance(JankHunterVariantConfiguration::class.java)
        }
        configuration.action()
    }

    fun variant(name: String, action: JankHunterVariantConfiguration.() -> Unit) {
        val normalized = name.trim().lowercase()
        require(normalized.isNotEmpty()) { "Jank Hunter variant name must not be blank." }
        val configuration = variantConfigurations.getOrPut(normalized) {
            objects.newInstance(JankHunterVariantConfiguration::class.java)
        }
        configuration.action()
    }

    internal fun resolve(identity: JankHunterVariantIdentity): JankHunterResolvedConfiguration {
        val layers = ArrayList<JankHunterVariantRule>(3 + identity.productFlavors.size)
        layers += JankHunterVariantRule(
            source = "global",
            priority = JankHunterConfigurationPriority.GLOBAL,
            configuration = conciseDefaults,
        )
        buildTypeConfigurations[normalizedBuildType(identity.buildType)]?.let { configuration ->
            layers += JankHunterVariantRule(
                source = "buildType(${identity.buildType})",
                priority = JankHunterConfigurationPriority.BUILD_TYPE,
                configuration = configuration,
            )
        }
        identity.productFlavors.forEach { (dimension, name) ->
            flavorConfigurations[dimension to name]?.let { configuration ->
                layers += JankHunterVariantRule(
                    source = "flavor($dimension=$name)",
                    priority = JankHunterConfigurationPriority.FLAVOR,
                    configuration = configuration,
                )
            }
        }
        variantConfigurations[identity.name.lowercase()]?.let { configuration ->
            layers += JankHunterVariantRule(
                source = "variant(${identity.name})",
                priority = JankHunterConfigurationPriority.EXACT_VARIANT,
                configuration = configuration,
            )
        }
        return JankHunterVariantConfigurationResolver.resolve(identity, layers)
    }

    internal fun isEnabledFor(identity: JankHunterVariantIdentity): Boolean {
        if (!enabled.getOrElse(true)) return false
        val buildType = normalizedBuildType(identity.buildType)
        return enabledBuildTypes.getOrElse(emptySet()).any { configured ->
            normalizedBuildType(configured) == buildType
        }
    }

    internal fun requiresDebugReleaseParity(identity: JankHunterVariantIdentity): Boolean {
        val buildType = normalizedBuildType(identity.buildType)
        if (buildType != "debug" && buildType != "release") return false
        val buildTypeOverride = buildTypeConfigurations[buildType]?.hasFunctionalOverrides() == true
        val exactVariantOverride = variantConfigurations[identity.name.lowercase()]?.hasFunctionalOverrides() == true
        return !buildTypeOverride && !exactVariantOverride
    }

    private fun buildTypeConfiguration(name: String): JankHunterVariantConfiguration {
        return buildTypeConfigurations.getOrPut(normalizedBuildType(name)) {
            objects.newInstance(JankHunterVariantConfiguration::class.java)
        }
    }

}
