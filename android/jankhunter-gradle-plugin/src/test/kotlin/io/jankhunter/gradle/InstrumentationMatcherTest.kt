package io.jankhunter.gradle

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class InstrumentationMatcherTest {
    @Test
    fun skipsApplicationClassesWhenIncludeListIsEmpty() {
        val matcher = InstrumentationMatcher(emptyList(), emptyList())

        assertFalse(matcher.matches("com.example.FeedPresenter"))
    }

    @Test
    fun excludesPlatformAndSdkClasses() {
        val matcher = InstrumentationMatcher(emptyList(), emptyList(), includeWholeApplication = true)

        assertFalse(matcher.matches("kotlin.collections.CollectionsKt"))
        assertFalse(matcher.matches("androidx.fragment.app.Fragment"))
        assertFalse(matcher.matches("io.jankhunter.runtime.JankHunter"))
        assertFalse(matcher.matches("io.jankhunter.okhttp3.JankHunterEventListener"))
        assertFalse(matcher.matches("io.jankhunter.workmanager.JankHunterCoroutineWorker"))
    }

    @Test
    fun allowsExplicitlyIncludedJankHunterSamplePackage() {
        val matcher = InstrumentationMatcher(
            includePackages = listOf("io.jankhunter.sample.graph"),
            excludePackages = emptyList(),
        )

        assertTrue(matcher.matches("io.jankhunter.sample.graph.CheckoutRepository"))
        assertFalse(matcher.matches("io.jankhunter.runtime.JankHunter"))
    }

    @Test
    fun wholeApplicationMatchesDependencyPackagesButKeepsSafetyExcludes() {
        val matcher = InstrumentationMatcher(
            includePackages = listOf("com.example"),
            excludePackages = listOf("org.example.generated"),
            includeWholeApplication = true,
        )

        assertTrue(matcher.matches("org.example.network.ReleaseNetworkClient"))
        assertFalse(matcher.matches("org.example.generated.NetworkFactory"))
        assertFalse(matcher.matches("okhttp3.OkHttpClient"))
        assertFalse(matcher.matches("io.jankhunter.runtime.JankHunter"))
    }

    @Test
    fun staticMatcherKeepsJankHunterHelpersOutsideWholeApplicationInstrumentation() {
        val excludedHelpers = listOf(
            "io.jankhunter.okhttp3.JankHunterOkHttp3",
            "io.jankhunter.runtime.JankHunterHooks",
            "io.jankhunter.workmanager.WorkerTelemetry",
        )

        excludedHelpers.forEach { className ->
            assertFalse(
                className,
                InstrumentationMatcher.matchesNormalizedClassName(
                    normalizedClassName = className,
                    includePackages = emptySet(),
                    excludePackages = emptySet(),
                    includeWholeApplication = true,
                ),
            )
        }
    }

    @Test
    fun staticMatcherDoesNotExcludePackagesThatOnlyShareANamePrefix() {
        assertTrue(
            InstrumentationMatcher.matchesNormalizedClassName(
                normalizedClassName = "io.jankhunter.okhttp3extra.ApplicationClient",
                includePackages = emptySet(),
                excludePackages = emptySet(),
                includeWholeApplication = true,
            ),
        )
    }

    @Test
    fun honorsIncludeAndExcludePackages() {
        val matcher = InstrumentationMatcher(
            includePackages = listOf("com.example"),
            excludePackages = listOf("com.example.generated"),
        )

        assertTrue(matcher.matches("com.example.feature.CheckoutPresenter"))
        assertFalse(matcher.matches("com.example.generated.R"))
        assertTrue(matcher.matches("com.example.generated2.RealClass"))
        assertFalse(matcher.matches("com.examples.Feature"))
        assertFalse(matcher.matches("com.other.Feature"))
    }

    @Test
    fun skipsGeneratedAndroidClassesEvenWhenPackageMatches() {
        val matcher = InstrumentationMatcher(
            includePackages = listOf("com.example"),
            excludePackages = emptyList(),
        )

        assertFalse(matcher.matches("com.example.R"))
        assertFalse(matcher.matches("com.example.R\$string"))
        assertFalse(matcher.matches("com.example.BuildConfig"))
        assertFalse(matcher.matches("com.example.Manifest\$permission"))
        assertFalse(matcher.matches("com.example.BR"))
        assertTrue(matcher.matches("com.example.feature.CheckoutPresenter"))
    }
}
