package io.jankhunter.gradle

import java.io.File
import org.gradle.testkit.runner.GradleRunner
import org.junit.Assert.assertTrue
import org.junit.Test

class MetadataReaderServiceIntegrationTest {
    @Test fun readerIsClosedForDirectTransformsInRepeatedDaemonBuilds() {
        val sdk = System.getenv("ANDROID_HOME") ?: System.getenv("ANDROID_SDK_ROOT")
        org.junit.Assume.assumeTrue("Android SDK required", !sdk.isNullOrBlank())
        val fixture = File("src/test/fixtures/metadata-compatibility").absoluteFile
        val runner = GradleRunner.create().withProjectDir(fixture).withPluginClasspath()
            .withArguments("--gradle-user-home", System.getenv("GRADLE_USER_HOME") ?: File(System.getProperty("user.home"), ".gradle").path,
                ":app:transformDebugClassesWithAsm", "--rerun-tasks", "--configuration-cache", "--console=plain")
        runner.build()
        repeat(2) { iteration ->
            val result = runner.withArguments(runner.arguments + "--offline").build()
            assertTrue("Direct transform must close its real reader: ${result.output}",
                result.output.contains("\"backend_open_count\":1"))
            if (iteration == 1) assertTrue(result.output, result.output.contains("Reusing configuration cache"))
        }
    }

    @Test fun parallelVariantsKeepSeparateReportsAndOneReaderLifetime() {
        val sdk = System.getenv("ANDROID_HOME") ?: System.getenv("ANDROID_SDK_ROOT")
        org.junit.Assume.assumeTrue("Android SDK required", !sdk.isNullOrBlank())
        val fixture = File("src/test/fixtures/metadata-compatibility").absoluteFile
        val result = GradleRunner.create().withProjectDir(fixture).withPluginClasspath()
            .withArguments("--gradle-user-home", System.getenv("GRADLE_USER_HOME") ?: File(System.getProperty("user.home"), ".gradle").path,
                ":app:assembleDebug", ":app:assembleRelease", "-PfixtureParallelVariants=true", "--parallel", "--rerun-tasks", "--configuration-cache", "--console=plain")
            .build()
        assertTrue(result.output, result.output.contains("\"backend_open_count\":1"))
        for (variant in listOf("debug", "release")) {
            val output = fixture.resolve("app/build/generated/jankhunter/$variant")
            val metadata = output.resolve("artifact-metadata.json").readText()
            assertTrue(metadata, metadata.contains("\"variant\":\"$variant\""))
            val summary = output.resolve("metadata-compatibility.json").readText()
            assertTrue(summary, summary.contains("\"incomplete\":false"))
            val observations = Regex("\"observations\":([0-9]+)").find(summary)?.groupValues?.get(1)?.toLong()
            assertTrue(summary, observations != null && observations > 0)
        }
    }

    @Test
    fun hookAbiIndexRoundTripsWithoutLiveProjectState() {
        val owner = "io/jankhunter/runtime/JankHunterHooks"
        val header = HookAbiClass(
            org.objectweb.asm.Opcodes.ACC_PUBLIC,
            "java/lang/Object",
            emptyList(),
            mapOf("hook()V" to (org.objectweb.asm.Opcodes.ACC_PUBLIC or org.objectweb.asm.Opcodes.ACC_STATIC)),
            emptyMap(),
        )
        val file = kotlin.io.path.createTempFile("jankhunter-hook-abi", ".bin").toFile()
        try {
            HookAbiIndex(mapOf(owner to header), deferred = false).write(file)
            val restored = HookAbiIndex.read(file)
            restored.validateMethod(org.objectweb.asm.Opcodes.INVOKESTATIC, owner, "hook", "()V")
        } finally {
            file.delete()
        }
    }

    @Test
    fun configurationCacheStoresAndReusesConcurrencyFixture() {
        val sdk = System.getenv("ANDROID_HOME") ?: System.getenv("ANDROID_SDK_ROOT")
        org.junit.Assume.assumeTrue("Android SDK required for configuration-cache integration", !sdk.isNullOrBlank())
        val projectDir = File("src/test/fixtures/metadata-compatibility").absoluteFile
        assertTrue(projectDir.isDirectory)
        val runner = GradleRunner.create()
            .withProjectDir(projectDir)
            .withPluginClasspath()
            .forwardOutput()
        val arguments = listOf("--gradle-user-home", System.getenv("GRADLE_USER_HOME") ?: File(System.getProperty("user.home"), ".gradle").path,
            ":app:assembleDebug", "--configuration-cache", "--stacktrace", "--console=plain")
        runner.withArguments(arguments).build()
        runner.withArguments(arguments + "--offline").build()
        val warm = runner.withArguments(arguments + "--offline").build()
        assertTrue(
            warm.output.contains("Reusing configuration cache"),
        )
    }
}
