package io.jankhunter.gradle

import org.gradle.testfixtures.ProjectBuilder
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.nio.file.Files

class MergeJankHunterInstrumentationArtifactsTaskTest {
    @Test
    fun metadataCompatibilitySummaryIsRestoredFromDiagnosticShards() {
        val project = ProjectBuilder.builder().build()
        val diagnosticsDir = Files.createTempDirectory("jankhunter-diagnostics-shards").toFile()
        val shard = diagnosticsDir.resolve("shard-0.jsonl")
        shard.parentFile.mkdirs()
        shard.writeText(
            "{\"format\":2,\"pass\":\"main\",\"metadataStatus\":\"READABLE\",\"metadataVersion\":\"2.1.0\"," +
                "\"metadataReader\":\"kotlin-jvm-2.3.20\",\"class\":\"com.example.KotlinType\"}\n",
        )
        val metadataOut = project.layout.buildDirectory.file("jankhunter/metadata-compatibility.json")
        val diagnosticsOut = project.layout.buildDirectory.file("jankhunter/instrumentation-diagnostics.jsonl")
        val classGraphOut = project.layout.buildDirectory.file("jankhunter/class-graph.jsonl")
        val lambdaOut = project.layout.buildDirectory.file("jankhunter/lambda-captures.jsonl")
        val androidOut = project.layout.buildDirectory.file("jankhunter/android-components.jsonl")
        val task = project.tasks.register(
            "mergeInstrumentationArtifacts",
            MergeJankHunterInstrumentationArtifactsTask::class.java,
        ).get()
        task.diagnosticsDirectory.set(diagnosticsDir)
        task.diagnosticsFiles.from(shard)
        task.metadataCompatibilityOutputFile.set(metadataOut)
        task.classGraphOutputFile.set(classGraphOut)
        task.lambdaCaptureOutputFile.set(lambdaOut)
        task.diagnosticsOutputFile.set(diagnosticsOut)
        task.androidComponentCatalogOutputFile.set(androidOut)

        task.merge()

        val json = metadataOut.get().asFile.readText()
        assertTrue(json.contains("\"schema\":1"))
        assertTrue(json.contains("\"status\":\"READABLE\""))
        assertTrue(json.contains("com.example.KotlinType"))
        assertEquals(
            shard.readText().trim(),
            diagnosticsOut.get().asFile.readText().trim(),
        )
    }
}
