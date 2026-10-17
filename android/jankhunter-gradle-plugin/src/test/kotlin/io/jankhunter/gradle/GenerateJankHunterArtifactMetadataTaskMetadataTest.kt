package io.jankhunter.gradle

import org.gradle.testfixtures.ProjectBuilder
import org.junit.Assert.assertTrue
import org.junit.Test

class GenerateJankHunterArtifactMetadataTaskMetadataTest {
    @Test
    fun artifactMetadataRecordsAdaptiveReaderIdentity() {
        val project = ProjectBuilder.builder().build()
        val task = project.tasks.register("artifactMetadata", GenerateJankHunterArtifactMetadataTask::class.java).get()
        task.variantName.set("debug")
        task.methodCounters.set(false)
        task.okhttp.set(false)
        task.webSockets.set(false)
        task.handlers.set(false)
        task.executors.set(false)
        task.threadLifecycle.set(false)
        task.concurrencyDiagnostics.set(false)
        task.coroutines.set(false)
        task.interactionOperations.set(false)
        task.lifecycleLeaks.set(false)
        task.logSpam.set(false)
        task.classGraph.set(false)
        task.runtimeCallGraph.set(false)
        task.symbolNamespace.set("test")
        task.includeWholeApplication.set(false)
        task.networkWholeApplication.set(true)
        task.databaseWholeApplication.set(true)
        task.databaseTracing.set(false)
        task.ioTracing.set(false)
        task.androidNamespace.set("com.example")
        task.includePackages.set(emptySet())
        task.excludePackages.set(emptySet())
        task.metadataCompatibilityMode.set(JankHunterMetadataCompatibilityMode.STRICT)
        task.metadataReaderIdentity.set(MetadataReaderCapabilities.identity)
        task.outputFile.set(project.layout.buildDirectory.file("artifact-metadata.json"))
        task.write()
        val json = task.outputFile.get().asFile.readText()
        assertTrue(json.contains("\"metadataCompatibilityMode\":\"STRICT\""))
        assertTrue(json.contains("\"metadataReaderIdentity\":\""))
        assertTrue(json.contains(MetadataReaderCapabilities.identity))
    }

    @Test
    fun metadataCompatibilitySummaryRestoresBoundedGroupsFromDiagnosticShards() {
        val summary = MetadataCompatibilitySummary()
        summary.accept(
            "{\"metadataStatus\":\"UNSUPPORTED_VERSION\",\"metadataVersion\":\"2.4.0\",\"metadataReader\":\"\",\"class\":\"example.Future\"}",
        )
        summary.accept(
            "{\"metadataStatus\":\"ABSENT\",\"metadataVersion\":\"\",\"metadataReader\":\"\",\"class\":\"example.Java\"}",
        )
        val json = summary.toJson()
        assertTrue(json.contains("\"incomplete\":true"))
        assertTrue(json.contains("\"status\":\"ABSENT\""))
        assertTrue(json.contains("\"status\":\"UNSUPPORTED_VERSION\""))
        assertTrue(json.contains("example.Future"))
    }
}
