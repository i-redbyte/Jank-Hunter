package io.jankhunter.gradle

import org.gradle.testfixtures.ProjectBuilder
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class GenerateJankHunterBuildIdentityTaskTest {
    @Test fun hashesExactMappingBytesAndOverwritesMappedIdentityForUnminifiedBuild() {
        val project = ProjectBuilder.builder().build()
        val task = project.tasks.register("identity", GenerateJankHunterBuildIdentityTask::class.java).get()
        val mapping = project.file("mapping.txt").apply { writeText("abc") }
        task.mappingFile.set(mapping)
        task.minified.set(true)
        task.symbolNamespace.set("0123456789abcdef0123456789abcdef")
        task.assetsDirectory.set(project.layout.buildDirectory.dir("identity"))
        task.generate()
        val asset = task.assetsDirectory.file(GenerateJankHunterBuildIdentityTask.ASSET_PATH).get().asFile
        assertEquals(
            "schema=1\nstate=mapped\nmapping-sha256=ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad\n" +
                "symbol-namespace=0123456789abcdef0123456789abcdef\n",
            asset.readText(),
        )
        task.mappingFile.unset()
        task.minified.set(false)
        task.generate()
        assertEquals(
            "schema=1\nstate=unminified\nmapping-sha256=\nsymbol-namespace=0123456789abcdef0123456789abcdef\n",
            asset.readText(),
        )
    }

    @Test fun minifiedBuildCannotClaimIdentityWithoutMapping() {
        val project = ProjectBuilder.builder().build()
        val task = project.tasks.register("identity", GenerateJankHunterBuildIdentityTask::class.java).get()
        task.minified.set(true)
        assertThrows(IllegalStateException::class.java) { task.generate() }
    }
}
