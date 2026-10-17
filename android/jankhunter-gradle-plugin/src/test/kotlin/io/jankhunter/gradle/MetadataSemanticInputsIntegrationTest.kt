package io.jankhunter.gradle

import java.io.ByteArrayOutputStream
import java.io.File
import java.util.Properties
import java.util.zip.ZipEntry
import java.util.zip.ZipOutputStream
import org.gradle.testkit.runner.GradleRunner
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test

class MetadataSemanticInputsIntegrationTest {
    @Test fun missingProjectSdkJarIsBuiltBeforeTransformParametersAreIsolated() = withFixture { root, _ ->
        val runner = runner(root)
        runner.build()
        assertTrue(root.resolve("jankhunter-runtime/build").deleteRecursively())
        val rebuilt = runner.build()
        assertTrue(rebuilt.output, root.resolve("jankhunter-runtime/build/libs/jankhunter-runtime.jar").isFile)
    }

    @Test fun changedSdkMethodCannotReuseValidatedOutputs() = withFixture { root, _ ->
        val runner = runner(root)
        runner.build()
        val summary = summary(root).readText()
        val sdk = root.resolve("jankhunter-runtime/src/main/java/io/jankhunter/runtime/JankHunterHooks.java")
        val original = sdk.readText()
        val changed = original.replace("long enterMethod(", "long removedEnterMethod(")
        assertNotEquals(original, changed)
        sdk.writeText(changed)
        val failure = runner.buildAndFail()
        assertTrue(failure.output, failure.output.contains("JankHunterHooks") && failure.output.contains("enterMethod"))
        sdk.writeText(original)
        runner.build()
        assertEquals(summary, summary(root).readText())
    }

    @Test fun changedReaderPackageRevisionCannotReuseOldIdentityOrReport() = withFixture { root, temporary ->
        val runner = runner(root)
        val sourceClasspath = runner.pluginClasspath.toList()
        val originalClasspath = packagedClasspath(sourceClasspath, temporary.resolve("original.jar"))
        val alternateClasspath = packagedClasspath(sourceClasspath, temporary.resolve("alternate.jar"), "alternate")
        runner.withPluginClasspath(originalClasspath).build()
        val originalReport = summary(root).readText()
        val originalMetadata = metadata(root).readText()
        val buildScript = root.resolve("build.gradle.kts")
        val originalBuildScript = buildScript.readText()
        // TestKit injection is outside the script inputs; model a declared plugin package revision.
        buildScript.writeText(originalBuildScript + "\nextra[\"fixtureReaderRevision\"] = \"alternate\"\n")
        runner.withPluginClasspath(alternateClasspath).build()
        assertTrue(summary(root).readText(), summary(root).readText().contains("\"reader\":\"alternate\""))
        assertNotEquals(readerIdentity(originalMetadata), readerIdentity(metadata(root).readText()))
        buildScript.writeText(originalBuildScript)
        runner.withPluginClasspath(originalClasspath).build()
        assertEquals(originalReport, summary(root).readText())
        assertEquals(readerIdentity(originalMetadata), readerIdentity(metadata(root).readText()))
    }

    private fun packagedClasspath(classpath: List<File>, target: File, readerId: String? = null): List<File> {
        val roots = classpath.filter { entry -> listOf("classes/java/main", "classes/kotlin/main", "resources/main")
            .any { entry.invariantSeparatorsPath.endsWith(it) } }
        check(roots.any { it.resolve(CAPABILITIES).isFile })
        ZipOutputStream(target.outputStream()).use { zip ->
            for (root in roots) for (file in root.walkTopDown().filter(File::isFile)) {
                val name = file.relativeTo(root).invariantSeparatorsPath
                var bytes = file.readBytes()
                if (name == CAPABILITIES && readerId != null) {
                    val properties = Properties().apply { bytes.inputStream().use(::load) }
                    properties.setProperty("reader.id", readerId)
                    bytes = ByteArrayOutputStream().also { properties.store(it, "Reader revision fixture") }.toByteArray()
                }
                zip.putNextEntry(ZipEntry(name)); zip.write(bytes); zip.closeEntry()
            }
        }
        return listOf(target) + classpath.filterNot { it in roots }
    }

    private fun runner(root: File): GradleRunner = GradleRunner.create().withProjectDir(root).withPluginClasspath()
        .withArguments("--gradle-user-home", System.getenv("GRADLE_USER_HOME") ?: File(System.getProperty("user.home"), ".gradle").path,
            ":app:assembleDebug", "--configuration-cache", "--build-cache", "--stacktrace", "--console=plain")

    private fun summary(root: File) = root.resolve("app/build/generated/jankhunter/debug/metadata-compatibility.json")
    private fun metadata(root: File) = root.resolve("app/build/generated/jankhunter/debug/artifact-metadata.json")
    private fun readerIdentity(text: String): String = checkNotNull(
        Regex("\"metadataReaderIdentity\":\"([a-f0-9]+)\"").find(text),
    ).groupValues[1]

    private fun withFixture(action: (File, File) -> Unit) {
        assumeTrue("Android SDK required", !(System.getenv("ANDROID_HOME") ?: System.getenv("ANDROID_SDK_ROOT")).isNullOrBlank())
        val temporary = kotlin.io.path.createTempDirectory("metadata-inputs").toFile()
        val source = File("src/test/fixtures/metadata-compatibility").absoluteFile
        val root = temporary.resolve("project")
        try {
            source.walkTopDown().onEnter { it.name !in setOf("build", ".gradle", ".kotlin") }.forEach { entry ->
                val target = root.resolve(entry.relativeTo(source).path)
                if (entry.isDirectory) target.mkdirs()
                else if (entry.name != "local.properties") entry.copyTo(target)
            }
            action(root, temporary)
        } finally { temporary.deleteRecursively() }
    }

    companion object {
        private const val CAPABILITIES = "META-INF/jankhunter/metadata-reader/capabilities.properties"
    }
}
