package io.jankhunter.gradle

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class InstrumentationMatcherTest {
    @Test
    fun skipsApplicationClassesWhenIncludeListIsEmptyByDefault() {
        val matcher = InstrumentationMatcher(emptyList(), emptyList())

        assertFalse(matcher.matches("com.example.FeedPresenter"))
    }

    @Test
    fun canAllowEmptyIncludeListExplicitly() {
        val matcher = InstrumentationMatcher(emptyList(), emptyList(), allowEmptyIncludes = true)

        assertTrue(matcher.matches("com.example.FeedPresenter"))
    }

    @Test
    fun excludesPlatformAndSdkClasses() {
        val matcher = InstrumentationMatcher(emptyList(), emptyList())

        assertFalse(matcher.matches("kotlin.collections.CollectionsKt"))
        assertFalse(matcher.matches("androidx.fragment.app.Fragment"))
        assertFalse(matcher.matches("io.jankhunter.runtime.JankHunter"))
    }

    @Test
    fun honorsIncludeAndExcludePackages() {
        val matcher = InstrumentationMatcher(
            includePackages = listOf("com.example"),
            excludePackages = listOf("com.example.generated"),
        )

        assertTrue(matcher.matches("com.example.feature.CheckoutPresenter"))
        assertFalse(matcher.matches("com.example.generated.R"))
        assertFalse(matcher.matches("com.other.Feature"))
    }
}
