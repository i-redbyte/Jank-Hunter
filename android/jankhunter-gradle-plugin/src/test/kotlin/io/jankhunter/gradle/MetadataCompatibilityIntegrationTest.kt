package io.jankhunter.gradle

import java.io.File
import org.gradle.testkit.runner.GradleRunner
import org.junit.Assume.assumeTrue
import org.junit.Assert.assertTrue
import org.junit.Test

class MetadataCompatibilityIntegrationTest {
    private val projectDir = File("src/test/fixtures/metadata-compatibility").absoluteFile

    @Test
    fun assembleDebugProducesMetadataCompatibilitySummary() {
        assumeAndroidSdk()
        assertTrue("metadata-compatibility fixture is required", projectDir.isDirectory)
        GradleRunner.create()
            .withProjectDir(projectDir)
            .withPluginClasspath()
            .forwardOutput()
            .withArguments("--gradle-user-home", System.getenv("GRADLE_USER_HOME") ?: File(System.getProperty("user.home"), ".gradle").path, ":app:assembleDebug", "--stacktrace", "--console=plain")
            .build()
        val summary = projectDir.resolve("app/build/generated/jankhunter/debug/metadata-compatibility.json")
        assertTrue("expected merged metadata summary", summary.isFile)
        val text = summary.readText()
        assertTrue(text.contains("\"schema\":1"))
        assertTrue(text.contains("\"metadataStatus\"") || text.contains("\"status\":\"READABLE\""))
    }

    private fun assumeAndroidSdk() {
        val sdk = System.getenv("ANDROID_HOME") ?: System.getenv("ANDROID_SDK_ROOT")
        assumeTrue("Android SDK required for metadata-compatibility integration", !sdk.isNullOrBlank())
    }
}
