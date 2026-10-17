package io.jankhunter.gradle

import java.io.File
import java.util.zip.ZipEntry
import java.util.zip.ZipOutputStream
import org.gradle.testkit.runner.GradleRunner
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Opcodes

class MetadataModeCacheIntegrationTest {
    @Test fun changingModeCannotReuseFutureRuntimeDependencyOutputInStrictBuild() {
        assumeTrue("Android SDK required", !(System.getenv("ANDROID_HOME") ?: System.getenv("ANDROID_SDK_ROOT")).isNullOrBlank())
        val jar = kotlin.io.path.createTempFile("future-metadata", ".jar").toFile()
        try {
            val source = ClassWriter(0).apply {
                visit(Opcodes.V1_8, Opcodes.ACC_PUBLIC, "com/fixture/app/FutureMetadata", null, "java/lang/Object", null)
                visitAnnotation(KotlinGeneratedMethodIndex.METADATA_DESCRIPTOR, true).apply {
                    visit("k", 1); visit("mv", intArrayOf(99, 0)); visitEnd()
                }
                visitEnd()
            }.toByteArray()
            ZipOutputStream(jar.outputStream()).use { zip ->
                zip.putNextEntry(ZipEntry("com/fixture/app/FutureMetadata.class"))
                zip.write(source); zip.closeEntry()
            }
            val fixture = File("src/test/fixtures/metadata-compatibility").absoluteFile
            val runner = GradleRunner.create().withProjectDir(fixture).withPluginClasspath()
            val arguments = listOf("--gradle-user-home", System.getenv("GRADLE_USER_HOME") ?: File(System.getProperty("user.home"), ".gradle").path,
                ":app:assembleDebug", "--console=plain", "--configuration-cache", "--build-cache", "-PfutureMetadataJar=${jar.path}")
            runner.withArguments(arguments + "-PfixtureMetadataMode=COMPATIBILITY").build()
            val offlineArguments = arguments + "--offline"
            val report = fixture.resolve("app/build/generated/jankhunter/debug/metadata-compatibility.json")
            val compatible = report.readText()
            assertTrue(compatible.contains("\"incomplete\":true"))
            assertTrue(compatible.contains("UNSUPPORTED_VERSION"))
            val failure = runner.withArguments(offlineArguments + "-PfixtureMetadataMode=STRICT").buildAndFail()
            assertTrue(failure.output, failure.output.contains("UNSUPPORTED_VERSION"))
            assertTrue(failure.output, failure.output.contains("FutureMetadata"))
            runner.withArguments(offlineArguments + "-PfixtureMetadataMode=COMPATIBILITY").build()
            assertEquals(compatible, report.readText())
        } finally { jar.delete() }
    }
}
