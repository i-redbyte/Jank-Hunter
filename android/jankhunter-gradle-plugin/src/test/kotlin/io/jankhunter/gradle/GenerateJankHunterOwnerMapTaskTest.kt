package io.jankhunter.gradle

import org.gradle.testfixtures.ProjectBuilder
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.nio.file.Files

class GenerateJankHunterOwnerMapTaskTest {
    @Test
    fun writesMetadataJsonlWithoutFakeOwnersObject() {
        val project = ProjectBuilder.builder().build()
        val task = project.tasks.register(
            "generateOwnerMap",
            GenerateJankHunterOwnerMapTask::class.java,
        ).get()
        task.outputFile.set(project.layout.buildDirectory.file("jankhunter/owner-map.json"))
        task.entriesDirectory.set(project.layout.buildDirectory.dir("jankhunter/owner-map-entries"))
        task.variantName.set("debug")
        task.methodCounters.set(true)
        task.okhttp.set(true)
        task.webSockets.set(true)
        task.handlers.set(true)
        task.executors.set(true)
        task.coroutines.set(false)
        task.flowInteractions.set(true)
        task.logSpam.set(true)
        task.classGraph.set(true)
        task.runtimeCallGraph.set(false)
        task.generatedOwners.set(true)
        task.allowEmptyIncludePackages.set(false)
        task.includeWholeApplication.set(true)
        task.androidNamespace.set("com.app")
        task.includePackages.set(listOf("com.app"))
        task.excludePackages.set(listOf("com.app.generated"))

        task.write()

        val text = task.outputFile.get().asFile.readText()
        assertTrue(text.contains("\"format\":2"))
        assertTrue(text.contains("\"kind\":\"metadata\""))
        assertTrue(text.contains("\"generatedOwners\":true"))
        assertTrue(text.contains("\"includePackages\":[\"com.app\"]"))
        assertFalse(text.contains("\"owners\":{}"))
    }

    @Test
    fun mergesGeneratedOwnerEntryShards() {
        val project = ProjectBuilder.builder().build()
        val entriesDirectory = Files.createTempDirectory("jankhunter-owner-map-entries").toFile()
        OwnerMapWriter.writeEntries(
            entriesDirectory.absolutePath,
            "com/example/Foo",
            listOf(
                OwnerMapEntry(
                    id = "abc",
                    owner = "com.example.Foo.load",
                    className = "com.example.Foo",
                    methodName = "load",
                    descriptor = "()V",
                ),
            ),
        )
        val task = project.tasks.register(
            "generateOwnerMapWithEntries",
            GenerateJankHunterOwnerMapTask::class.java,
        ).get()
        task.outputFile.set(project.layout.buildDirectory.file("jankhunter/owner-map-with-entries.json"))
        task.entriesDirectory.set(entriesDirectory)
        task.variantName.set("debug")
        task.methodCounters.set(true)
        task.okhttp.set(false)
        task.webSockets.set(false)
        task.handlers.set(false)
        task.executors.set(false)
        task.coroutines.set(false)
        task.flowInteractions.set(false)
        task.logSpam.set(false)
        task.classGraph.set(false)
        task.runtimeCallGraph.set(false)
        task.generatedOwners.set(true)
        task.allowEmptyIncludePackages.set(false)
        task.includeWholeApplication.set(false)
        task.androidNamespace.set("com.app")
        task.includePackages.set(listOf("com.app"))
        task.excludePackages.set(emptyList())

        task.write()

        val text = task.outputFile.get().asFile.readText()
        assertTrue(text.contains("\"kind\":\"metadata\""))
        assertTrue(text.contains("\"kind\":\"entry\""))
        assertTrue(text.contains("\"owner\":\"com.example.Foo.load\""))
    }
}
