package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterConfigurationResolverTest {
    @Test
    fun profilesHaveStableCollectionAndFeatureContracts() {
        val minimal = JankHunterProfile.MINIMAL.definition
        val balanced = JankHunterProfile.BALANCED.definition
        val full = JankHunterProfile.FULL.definition
        val releaseSafe = JankHunterProfile.RELEASE_SAFE.definition
        val targeted = JankHunterProfile.TARGETED.definition

        assertEquals(JankHunterCollection.BALANCED, minimal.collection)
        assertEquals(setOf(JankHunterFeature.JANK_STATS), minimal.features)
        assertTrue(JankHunterFeature.SQLITE in balanced.features)
        assertTrue(JankHunterFeature.ROOM in balanced.features)
        assertTrue(JankHunterFeature.HTTP in balanced.features)
        assertTrue(JankHunterFeature.ANDROID_COMPONENTS in balanced.features)
        assertFalse(JankHunterFeature.BINDER_IPC in balanced.features)
        assertEquals(JankHunterCollection.EXACT, full.collection)
        assertTrue(JankHunterFeature.DI_ANALYSIS in full.features)
        assertTrue(JankHunterFeature.CALL_GRAPH in full.features)
        assertTrue(JankHunterFeature.ANDROID_COMPONENTS in full.features)
        assertTrue(JankHunterFeature.BINDER_IPC in full.features)
        assertFalse(JankHunterFeature.HEAP_DUMPS in full.features)
        assertEquals(JankHunterCollection.BALANCED, releaseSafe.collection)
        assertFalse(JankHunterFeature.DI_ANALYSIS in releaseSafe.features)
        assertFalse(JankHunterFeature.CALL_GRAPH in releaseSafe.features)
        assertTrue(JankHunterFeature.ANDROID_COMPONENTS in releaseSafe.features)
        assertFalse(JankHunterFeature.BINDER_IPC in releaseSafe.features)
        assertEquals(JankHunterCollection.EXACT, targeted.collection)
        assertTrue(targeted.features.isEmpty())
    }

    @Test
    fun explicitFeatureOverridesApplyAfterMostSpecificProfile() {
        val resolved = JankHunterConfigurationResolver.resolveFeatures(
            listOf(
                JankHunterConfigurationLayer(
                    priority = JankHunterConfigurationPriority.GLOBAL,
                    source = "global",
                    disabled = setOf(JankHunterFeature.DI_ANALYSIS),
                ),
                JankHunterConfigurationLayer(
                    priority = JankHunterConfigurationPriority.BUILD_TYPE,
                    source = "buildType(debug)",
                    profile = JankHunterProfile.FULL,
                ),
                JankHunterConfigurationLayer(
                    priority = JankHunterConfigurationPriority.EXACT_VARIANT,
                    source = "variant(internalDebug)",
                    enabled = setOf(JankHunterFeature.DI_ANALYSIS),
                    disabled = setOf(JankHunterFeature.WEBSOCKETS),
                ),
            ),
        )

        assertEquals(JankHunterProfile.FULL, resolved.profile)
        assertTrue(JankHunterFeature.DI_ANALYSIS in resolved.enabled)
        assertFalse(JankHunterFeature.WEBSOCKETS in resolved.enabled)
        assertEquals("variant(internalDebug)", resolved.sources.getValue(JankHunterFeature.DI_ANALYSIS))
    }

    @Test
    fun featureBundlesExpandWithoutLeakingBundleTokensIntoResolvedFeatures() {
        val resolved = JankHunterConfigurationResolver.resolveFeatures(
            listOf(
                JankHunterConfigurationLayer(
                    priority = JankHunterConfigurationPriority.GLOBAL,
                    source = "global",
                    profile = JankHunterProfile.TARGETED,
                    enabled = setOf(
                        JankHunterFeatureBundle.NETWORK_ALL,
                        JankHunterFeatureBundle.DATABASE_ALL,
                    ),
                    disabled = setOf(JankHunterFeature.WEBSOCKETS),
                ),
            ),
        )

        assertEquals(
            setOf(
                JankHunterFeature.HTTP,
                JankHunterFeature.SQLITE,
                JankHunterFeature.ROOM,
            ),
            resolved.enabled,
        )
    }

    @Test
    fun androidSystemBundleExpandsIntoIndependentComponentAndBinderCapabilities() {
        val resolved = JankHunterConfigurationResolver.resolveFeatures(
            listOf(
                JankHunterConfigurationLayer(
                    priority = JankHunterConfigurationPriority.GLOBAL,
                    source = "global",
                    profile = JankHunterProfile.TARGETED,
                    enabled = setOf(JankHunterFeatureBundle.ANDROID_SYSTEM_ALL),
                    disabled = setOf(JankHunterFeature.BINDER_IPC),
                ),
            ),
        )

        assertTrue(JankHunterFeature.ANDROID_COMPONENTS in resolved.enabled)
        assertFalse(JankHunterFeature.BINDER_IPC in resolved.enabled)
    }

    @Test
    fun contradictoryFeatureDirectivesAtSamePriorityFailWithActionableMessage() {
        val error = runCatching {
            JankHunterConfigurationResolver.resolveFeatures(
                listOf(
                    JankHunterConfigurationLayer(
                        priority = JankHunterConfigurationPriority.FLAVOR,
                        source = "flavor(environment=internal)",
                        enabled = setOf(JankHunterFeature.SQLITE),
                    ),
                    JankHunterConfigurationLayer(
                        priority = JankHunterConfigurationPriority.FLAVOR,
                        source = "flavor(store=google)",
                        disabled = setOf(JankHunterFeature.SQLITE),
                    ),
                ),
            )
        }.exceptionOrNull()

        requireNotNull(error)
        assertTrue(error is JankHunterConfigurationException)
        assertTrue(error.message.orEmpty().contains("SQLITE was explicitly enabled and disabled"))
        assertTrue(error.message.orEmpty().contains("flavor(environment=internal)"))
        assertTrue(error.message.orEmpty().contains("flavor(store=google)"))
    }
}
