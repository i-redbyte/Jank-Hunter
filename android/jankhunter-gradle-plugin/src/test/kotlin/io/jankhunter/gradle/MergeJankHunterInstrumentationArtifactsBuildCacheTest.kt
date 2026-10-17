package io.jankhunter.gradle

import java.io.File
import org.gradle.testkit.runner.GradleRunner
import org.junit.Assume.assumeTrue
import org.junit.Assert.assertTrue
import org.junit.Test

/** Verifies Gradle build cache can restore merged metadata compatibility output. */
class MergeJankHunterInstrumentationArtifactsBuildCacheTest {
    @Test
    fun mergeTaskOutputRestoredFromBuildCacheAfterOutputDeletion() {
        val sdk = System.getenv("ANDROID_HOME") ?: System.getenv("ANDROID_SDK_ROOT")
        assumeTrue("Android SDK required", !sdk.isNullOrBlank())
        val projectDir = File("src/test/fixtures/metadata-compatibility").absoluteFile
        assertTrue(projectDir.isDirectory)
        val runner = GradleRunner.create()
            .withProjectDir(projectDir)
            .withPluginClasspath()
            .forwardOutput()
        runner.withArguments("--gradle-user-home", System.getenv("GRADLE_USER_HOME") ?: File(System.getProperty("user.home"), ".gradle").path, "clean", ":app:assembleDebug", "--build-cache", "--console=plain").build()
        val summary = projectDir.resolve("app/build/generated/jankhunter/debug/metadata-compatibility.json")
        assertTrue(summary.isFile)
        val before = summary.readText()
        val outputRoot = summary.parentFile
        val names = listOf("metadata-compatibility.json", "instrumentation-diagnostics.jsonl", "class-graph.jsonl",
            "lambda-captures.jsonl", "android-components-catalog.jsonl", "di-catalog.jsonl")
        val reports = names.associateWith { outputRoot.resolve(it).readText() }
        reports.forEach { (name, text) -> assertTrue("Fixture must exercise $name", text.isNotBlank()) }
        summary.delete()
        val rebuild = runner.withArguments("--gradle-user-home", System.getenv("GRADLE_USER_HOME") ?: File(System.getProperty("user.home"), ".gradle").path,
            ":app:mergeDebugJankHunterInstrumentationArtifacts",
            "--build-cache",
            "--offline", "--console=plain",
        ).build()
        assertTrue(
            rebuild.output,
            rebuild.task(":app:mergeDebugJankHunterInstrumentationArtifacts")?.outcome ==
                org.gradle.testkit.runner.TaskOutcome.FROM_CACHE,
        )
        assertTrue(summary.isFile && summary.readText() == before)

        runner.withArguments("--gradle-user-home", System.getenv("GRADLE_USER_HOME") ?: File(System.getProperty("user.home"), ".gradle").path,
            "clean", "--offline", "--console=plain").build()
        val restored = runner.withArguments("--gradle-user-home", System.getenv("GRADLE_USER_HOME") ?: File(System.getProperty("user.home"), ".gradle").path,
            ":app:assembleDebug", "--build-cache", "--offline", "--console=plain").build()
        assertTrue("Cache restore must not decode metadata", !restored.output.contains("JH_METADATA_METRICS"))
        reports.forEach { (name, text) -> org.junit.Assert.assertEquals("Restored $name", text, outputRoot.resolve(name).readText()) }
        org.junit.Assert.assertEquals("Full clean/cache restore must preserve coverage reports", before, summary.readText())
    }
}
