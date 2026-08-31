package io.jankhunter.gradle

import org.gradle.testfixtures.ProjectBuilder
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class GenerateJankHunterArtifactMetadataTaskTest {
    @Test
    fun writesOneCompactMetadataObjectWithoutSymbolEntries() {
        val project = ProjectBuilder.builder().build()
        val task = project.tasks.register(
            "generateArtifactMetadata",
            GenerateJankHunterArtifactMetadataTask::class.java,
        ).get()
        task.outputFile.set(project.layout.buildDirectory.file("jankhunter/artifact-metadata.json"))
        task.variantName.set("debug")
        task.methodCounters.set(true)
        task.okhttp.set(true)
        task.webSockets.set(true)
        task.handlers.set(true)
        task.executors.set(true)
        task.coroutines.set(false)
        task.interactionOperations.set(true)
        task.lifecycleLeaks.set(true)
        task.logSpam.set(true)
        task.classGraph.set(true)
        task.runtimeCallGraph.set(false)
        task.symbolNamespace.set("0123456789abcdef0123456789abcdef")
        task.includeWholeApplication.set(true)
        task.networkWholeApplication.set(true)
        task.databaseWholeApplication.set(true)
        task.databaseTracing.set(true)
        task.ioTracing.set(true)
        task.androidNamespace.set("com.app")
        task.includePackages.set(setOf("com.app"))
        task.excludePackages.set(setOf("com.app.generated"))

        task.write()

        val text = task.outputFile.get().asFile.readText()
        assertEquals(1, text.lineSequence().filter(String::isNotBlank).count())
        assertTrue(text.contains("\"format\":1"))
        assertTrue(text.contains("\"kind\":\"artifact-metadata\""))
        assertTrue(text.contains("\"symbolNamespace\":\"0123456789abcdef0123456789abcdef\""))
        assertTrue(text.contains("\"includeWholeApplication\":true"))
        assertTrue(text.contains("\"networkWholeApplication\":true"))
        assertTrue(text.contains("\"databaseWholeApplication\":true"))
        assertTrue(text.contains("\"databaseTracing\":true"))
        assertTrue(text.contains("\"ioTracing\":true"))
        assertTrue(text.contains("\"includePackages\":[\"com.app\"]"))
        assertFalse(text.contains("\"kind\":\"entry\""))
        assertFalse(text.contains("\"owner\""))
    }
}
