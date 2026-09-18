package io.jankhunter.gradle

import java.util.zip.ZipEntry
import java.util.zip.ZipOutputStream
import org.gradle.testfixtures.ProjectBuilder
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class GenerateJankHunterBuildManifestTaskTest {
    @Test fun bindsPackageMappingAndArtifactsAndRejectsStaleMapping() {
        val project = ProjectBuilder.builder().build()
        val namespace = "0123456789abcdef0123456789abcdef"
        val digest = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
        val mapping = project.file("mapping.txt").apply { writeText("abc") }
        val asset = project.file("identity.txt").apply { writeText(buildIdentityAssetText(digest, namespace)) }
        val metadata = project.file("artifact-metadata.json").apply { writeText("abc") }
        val apk = project.file("app.apk")
        ZipOutputStream(apk.outputStream()).use { zip ->
            zip.putNextEntry(ZipEntry("assets/${GenerateJankHunterBuildIdentityTask.ASSET_PATH}"))
            zip.write(asset.readBytes())
            zip.closeEntry()
        }
        val task = project.tasks.register("manifest", GenerateJankHunterBuildManifestTask::class.java).get()
        task.variantName.set("release")
        task.packageKind.set("apk")
        task.symbolNamespace.set(namespace)
        task.identityAsset.set(asset)
        task.mappingFile.set(mapping)
        task.apkDirectory.set(apk.parentFile)
        task.artifacts.from(metadata)
        task.outputFile.set(project.layout.buildDirectory.file("manifest.json"))
        task.generate()
        val text = task.outputFile.get().asFile.readText()
        assertTrue(text.contains("\"mappingSha256\":\"$digest\""))
        assertTrue(text.contains("\"file\":\"app.apk\",\"sha256\":\"${artifactSha256(apk)}\""))
        assertTrue(text.contains("\"file\":\"artifact-metadata.json\",\"sha256\":\"$digest\""))
        assertFalse(text.contains(project.projectDir.absolutePath))
        task.generate()
        assertEquals(text, task.outputFile.get().asFile.readText())
        mapping.writeText("different mapping")
        assertThrows(IllegalStateException::class.java) { task.generate() }
    }
}
