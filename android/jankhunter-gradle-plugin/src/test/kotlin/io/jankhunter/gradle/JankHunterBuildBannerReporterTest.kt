package io.jankhunter.gradle

import java.lang.reflect.Proxy
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import org.gradle.api.logging.Logger
import org.gradle.testfixtures.ProjectBuilder
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterBuildBannerReporterTest {
    @Test
    fun formatsCompactRequiredBanner() {
        assertEquals(
            "================JANK HUNTER 1.2.3 ENABLED================",
            JankHunterBuildBannerReporter.message("1.2.3"),
        )
    }

    @Test
    fun emitsOnlyOnceWhenVariantsExecuteConcurrently() {
        val reporter = JankHunterBuildBannerReporter("1.2.3")
        val output = ConcurrentLinkedQueue<String>()
        val executor = Executors.newFixedThreadPool(16)
        try {
            val futures = List(16) {
                executor.submit {
                    repeat(32) {
                        reporter.emitOnce(output::add)
                    }
                }
            }
            futures.forEach { future ->
                future.get(5, TimeUnit.SECONDS)
            }
        } finally {
            executor.shutdownNow()
            assertTrue(executor.awaitTermination(5, TimeUnit.SECONDS))
        }

        assertEquals(
            listOf("================JANK HUNTER 1.2.3 ENABLED================"),
            output.toList(),
        )
    }

    @Test
    fun emitsMandatoryBannerAtQuietLevelOnlyOnce() {
        val calls = ConcurrentLinkedQueue<Pair<String, String>>()
        val logger = Proxy.newProxyInstance(
            Logger::class.java.classLoader,
            arrayOf(Logger::class.java),
        ) { _, method, arguments ->
            if (method.name == "quiet") {
                calls.add(method.name to arguments.single() as String)
            }
            null
        } as Logger
        val reporter = JankHunterBuildBannerReporter("1.2.3")

        repeat(3) {
            reporter.emitOnce(logger)
        }

        assertEquals(
            listOf("quiet" to "================JANK HUNTER 1.2.3 ENABLED================"),
            calls.toList(),
        )
    }

    @Test
    fun usesPublishedPluginVersion() {
        val versionName = JankHunterDependencyCoordinates.load().version

        assertNotEquals("unspecified", versionName)
        assertTrue(JankHunterBuildBannerReporter.message(versionName).contains(versionName))
    }

    @Test
    fun attachesBannerOnlyToTheRequestedVariantPreBuild() {
        val project = ProjectBuilder.builder().build()
        val preDebugBuild = project.tasks.register("preDebugBuild")
        val preReleaseBuild = project.tasks.register("preReleaseBuild")

        JankHunterPlugin().configureBuildBanner(project, "debug")

        val bannerTask = project.tasks.named("printDebugJankHunterBuildBanner").get()
        assertTrue(
            preDebugBuild.get().taskDependencies
                .getDependencies(preDebugBuild.get())
                .contains(bannerTask),
        )
        assertTrue(
            preReleaseBuild.get().taskDependencies
                .getDependencies(preReleaseBuild.get())
                .isEmpty(),
        )
    }
}
