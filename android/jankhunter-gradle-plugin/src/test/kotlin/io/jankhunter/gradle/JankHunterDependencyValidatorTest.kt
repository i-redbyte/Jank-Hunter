package io.jankhunter.gradle

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterDependencyValidatorTest {
    @Test
    fun detectsOkHttpAndHelperArtifacts() {
        val components = listOf(
            "project :app",
            "com.squareup.okhttp3:okhttp:3.12.13",
            "io.jankhunter:jankhunter-okhttp3:1.0.0",
        )

        assertTrue(JankHunterDependencyValidator.hasOkHttp(components))
        assertTrue(JankHunterDependencyValidator.hasJankHunterOkHttp3(components))
    }

    @Test
    fun doesNotRequireHelperWhenOkHttpIsAbsent() {
        val components = listOf(
            "project :app",
            "io.jankhunter:jankhunter-runtime:1.0.0",
        )

        assertFalse(JankHunterDependencyValidator.hasOkHttp(components))
        assertFalse(JankHunterDependencyValidator.hasJankHunterOkHttp3(components))
    }

    @Test
    fun detectsProjectHelperDependency() {
        val components = listOf(
            "project :sample-app",
            "com.squareup.okhttp3:okhttp:3.12.13",
            "project :jankhunter-okhttp3",
        )

        assertTrue(JankHunterDependencyValidator.hasOkHttp(components))
        assertTrue(JankHunterDependencyValidator.hasJankHunterOkHttp3(components))
    }
}
