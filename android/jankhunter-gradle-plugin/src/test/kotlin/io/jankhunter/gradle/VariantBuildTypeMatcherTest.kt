package io.jankhunter.gradle

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class VariantBuildTypeMatcherTest {
    @Test
    fun matchesExactBuildTypeAndFlavorPrefixedVariant() {
        assertTrue(VariantBuildTypeMatcher.isEnabled("debug", listOf("debug")))
        assertTrue(VariantBuildTypeMatcher.isEnabled("freeDebug", listOf("debug")))
        assertTrue(VariantBuildTypeMatcher.isEnabled("paidQaDebug", listOf("qaDebug")))
    }

    @Test
    fun doesNotMatchBuildTypeSubstringInFlavorOrCustomVariantName() {
        assertFalse(VariantBuildTypeMatcher.isEnabled("debuggableRelease", listOf("debug")))
        assertFalse(VariantBuildTypeMatcher.isEnabled("qaDebugRelease", listOf("debug")))
        assertFalse(VariantBuildTypeMatcher.isEnabled("freeRelease", listOf("debug")))
    }

    @Test
    fun identifiesReleaseLikeVariantsForSafetyGate() {
        assertTrue(VariantBuildTypeMatcher.isReleaseLike("release"))
        assertTrue(VariantBuildTypeMatcher.isReleaseLike("paidRelease"))
        assertFalse(VariantBuildTypeMatcher.isReleaseLike("releaseCandidateDebug"))
    }
}
