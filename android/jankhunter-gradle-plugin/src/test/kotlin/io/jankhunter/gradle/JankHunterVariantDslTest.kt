package io.jankhunter.gradle

import org.gradle.api.Project
import org.gradle.testfixtures.ProjectBuilder
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterVariantDslTest {
    @Test
    fun conciseDslResolvesGlobalBuildTypeFlavorAndExactVariantLayers() {
        val project = ProjectBuilder.builder().build()
        val extension = extension(project)

        extension.enabledBuildTypes.set(setOf("debug", "release"))
        extension.packages("ru.mail", " com.icq ")
        extension.storageLimitMiB(50)
        extension.enable(JankHunterFeatureBundle.NETWORK_ALL)
        extension.buildType("debug", JankHunterProfile.FULL) {
            disable(JankHunterFeature.DI_ANALYSIS)
            disable(JankHunterFeature.SQLITE)
        }
        extension.buildType("release", JankHunterProfile.RELEASE_SAFE) {
            enable(JankHunterFeature.DI_ANALYSIS)
        }
        extension.release {
            privacyReviewed()
            performanceBudget(project.file("release-budget.txt"))
        }
        extension.flavor("environment", "internal") {
            enable(JankHunterFeatureBundle.DATABASE_ALL)
        }
        extension.variant("internalDebug") {
            disable(JankHunterFeature.SQLITE)
        }
        extension.variant("internalRelease") {
            collection.set(JankHunterCollection.EXACT)
            disable(JankHunterFeature.WEBSOCKETS)
        }

        val debug = extension.resolve(
            JankHunterVariantIdentity("internalDebug", "debug", mapOf("environment" to "internal")),
        )
        val release = extension.resolve(
            JankHunterVariantIdentity("internalRelease", "release", mapOf("environment" to "internal")),
        )

        assertEquals(JankHunterProfile.FULL, debug.profile)
        assertFalse(JankHunterFeature.DI_ANALYSIS in debug.features)
        assertFalse(JankHunterFeature.SQLITE in debug.features)
        assertFalse(debug.instrumentation.databaseTracing)
        assertTrue(debug.instrumentation.roomTracing)
        assertEquals(JankHunterProfile.RELEASE_SAFE, release.profile)
        assertEquals(JankHunterCollection.EXACT, release.collection)
        assertTrue(JankHunterFeature.DI_ANALYSIS in release.features)
        assertFalse(JankHunterFeature.WEBSOCKETS in release.features)
        assertEquals(setOf("ru.mail", "com.icq"), release.includePackages)
        assertTrue(release.releaseSafety.privacyReviewed)
        assertTrue(release.releaseSafety.instrumentationApproved)
    }

    @Test
    fun releaseApprovalBlockInheritsGlobalBehaviorForDebugAndRelease() {
        val project = ProjectBuilder.builder().build()
        val extension = extension(project)
        extension.enabledBuildTypes.set(setOf("debug", "release"))
        extension.profile.set(JankHunterProfile.FULL)
        extension.disable(JankHunterFeature.DI_ANALYSIS)
        extension.processes.set(JankHunterProcesses.ALL)
        extension.release {
            privacyReviewed()
            allowSecondaryProcesses()
            performanceBudget(project.file("release-budget.txt"))
        }

        val debug = extension.resolve(JankHunterVariantIdentity("debug", "debug"))
        val release = extension.resolve(JankHunterVariantIdentity("release", "release"))

        assertEquals(JankHunterProfile.FULL, debug.profile)
        assertEquals(JankHunterProfile.FULL, release.profile)
        assertFalse(JankHunterFeature.DI_ANALYSIS in debug.features)
        assertFalse(JankHunterFeature.DI_ANALYSIS in release.features)
        assertTrue(JankHunterFunctionalParity.differences(debug, release).isEmpty())
        assertTrue(release.releaseSafety.instrumentationApproved)
        assertTrue(release.releaseSafety.allowSecondaryProcesses)
    }

    @Test
    fun explicitBuildTypeBehaviorDisablesAutomaticDebugReleaseParity() {
        val extension = extension()
        extension.buildType("debug", JankHunterProfile.FULL)
        extension.release { }

        assertFalse(extension.requiresDebugReleaseParity(JankHunterVariantIdentity("debug", "debug")))
        assertTrue(extension.requiresDebugReleaseParity(JankHunterVariantIdentity("release", "release")))
    }

    @Test
    fun scalarPropertiesOverrideProfilesAndGrowthAnalyticsDefaultsToEnabled() {
        val extension = extension()

        extension.profile.set(JankHunterProfile.MINIMAL)
        extension.disable(JankHunterFeature.LOGGING)
        extension.processes.set(JankHunterProcesses.ALL)
        extension.collection.set(JankHunterCollection.EXACT)
        extension.tuning.queueCapacity.set(4_096)

        val resolved = extension.resolve(JankHunterVariantIdentity("debug", "debug"))

        assertEquals(JankHunterProcesses.ALL, resolved.processes)
        assertEquals(JankHunterCollection.EXACT, resolved.collection)
        assertEquals(4_096, resolved.maxQueueSize)
        assertTrue(resolved.growthAnalytics)

        extension.growthAnalytics.set(false)

        assertFalse(extension.resolve(JankHunterVariantIdentity("debug", "debug")).growthAnalytics)
    }

    @Test
    fun typedScopeAndExpertTuningResolveWithoutTopLevelImplementationFlags() {
        val extension = extension()

        extension.scope.set(JankHunterInstrumentationScope.PACKAGES_ONLY)
        extension.tuning.apply {
            methodFiltering.set(JankHunterMethodFilterMode.REPORT_ONLY)
            thresholds.mainThreadStallMs.set(900L)
            thresholds.ownerBlockMs.set(300L)
            thresholds.slowHttpMs.set(1_500L)
            thresholds.jankFrameMs.set(48L)
            thresholds.uiWindowP95Ms.set(40L)
            admission.mainThreadWaitMs.set(1L)
            admission.backgroundWaitMs.set(8L)
            heapDumps.minIntervalMs.set(120_000L)
            heapDumps.maxCount.set(2)
            heapDumps.minRetainedAgeMs.set(45_000L)
        }

        val resolved = extension.resolve(JankHunterVariantIdentity("debug", "debug"))

        assertEquals(JankHunterInstrumentationScope.PACKAGES_ONLY, resolved.instrumentation.scope)
        assertEquals(JankHunterMethodFilterMode.REPORT_ONLY, resolved.instrumentation.methodFilterMode)
        assertEquals(900L, resolved.runtime.mainThreadStallThresholdMs)
        assertEquals(300L, resolved.runtime.ownerBlockThresholdMs)
        assertEquals(1_500L, resolved.runtime.httpSlowThresholdMs)
        assertEquals(48L, resolved.runtime.jankFrameThresholdMs)
        assertEquals(40L, resolved.runtime.uiWindowP95ThresholdMs)
        assertEquals(1L, resolved.runtime.mainThreadAdmissionWaitMs)
        assertEquals(8L, resolved.runtime.backgroundAdmissionWaitMs)
        assertEquals(120_000L, resolved.retainedHeapDump.minIntervalMs)
        assertEquals(2, resolved.retainedHeapDump.maxCount)
        assertEquals(45_000L, resolved.retainedHeapDump.minRetainedAgeMs)
    }

    @Test
    fun storageDslCannotRepresentDisabledLimitWithDormantSize() {
        val extension = extension()

        extension.unlimitedStorage()
        val unlimited = extension.resolve(JankHunterVariantIdentity("debug", "debug"))
        extension.storageLimitMiB(24)
        val limited = extension.resolve(JankHunterVariantIdentity("debug", "debug"))

        assertEquals(JankHunterStorage.Unlimited, unlimited.storage)
        assertEquals(JankHunterStorage.Limited(24), limited.storage)
    }

    @Test
    fun conflictingScalarFlavorOverridesFailWithSourcesAndSuggestedFix() {
        val extension = extension()

        extension.flavor("environment", "internal") {
            processes.set(JankHunterProcesses.ALL)
        }
        extension.flavor("store", "google") {
            processes.set(JankHunterProcesses.MAIN_ONLY)
        }

        val error = runCatching {
            extension.resolve(
                JankHunterVariantIdentity(
                    name = "internalGoogleDebug",
                    buildType = "debug",
                    productFlavors = mapOf("environment" to "internal", "store" to "google"),
                ),
            )
        }.exceptionOrNull()

        requireNotNull(error)
        assertTrue(error is JankHunterConfigurationException)
        assertTrue(error.message.orEmpty().contains("processes"))
        assertTrue(error.message.orEmpty().contains("flavor(environment=internal)"))
        assertTrue(error.message.orEmpty().contains("flavor(store=google)"))
        assertTrue(error.message.orEmpty().contains("exact variant override"))
    }

    @Test
    fun enabledBuildTypesRemainExplicitAndDefaultToDebug() {
        val extension = extension()

        assertEquals(setOf("debug"), extension.enabledBuildTypes.get())
        assertTrue(extension.isEnabledFor(JankHunterVariantIdentity("debug", "debug")))
        assertFalse(extension.isEnabledFor(JankHunterVariantIdentity("release", "release")))

        extension.enabledBuildTypes.set(setOf("debug", "release"))

        assertTrue(extension.isEnabledFor(JankHunterVariantIdentity("internalRelease", "release")))
    }

    private fun extension(project: Project = ProjectBuilder.builder().build()): JankHunterExtension {
        return project.objects.newInstance(JankHunterExtension::class.java)
    }
}
