package io.jankhunter.gradle

import org.gradle.testfixtures.ProjectBuilder
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.nio.file.Files

class MergeJankHunterDependencyInjectionCatalogTaskTest {
    @Test
    fun writesMetadataFirstAndRemovesDisabledOutput() {
        val project = ProjectBuilder.builder().build()
        val shards = Files.createTempDirectory("jankhunter-di-shards").toFile()
        DependencyInjectionCatalogWriter.write(
            shards.absolutePath,
            "com/app/Consumer",
            DependencyInjectionClassRecord(
                name = "com.app.Consumer",
                framework = DependencyInjectionFramework.DAGGER2,
                roles = setOf("consumer"),
                generated = false,
            ),
            listOf(
                DependencyInjectionEdgeRecord(
                    consumer = "com.app.Consumer",
                    dependency = "com.app.Dependency",
                    framework = DependencyInjectionFramework.DAGGER2,
                    injectionKind = "constructor",
                    site = "com.app.Consumer#<init>(Lcom/app/Dependency;)V",
                    resolution = "declared",
                ),
            ),
        )
        val task = project.tasks.register(
            "mergeDependencyInjectionCatalog",
            MergeJankHunterDependencyInjectionCatalogTask::class.java,
        ).get()
        task.analysisEnabled.set(true)
        task.variantName.set("debug")
        task.shardsDirectory.set(shards)
        task.shardFiles.from(shards.walkTopDown().filter { it.isFile }.toList())
        task.outputFile.set(project.layout.buildDirectory.file("jankhunter/di-catalog.jsonl"))

        task.merge()

        val output = task.outputFile.get().asFile
        val lines = output.readLines()
        assertTrue(lines.first().contains("\"kind\":\"metadata\""))
        assertTrue(lines.first().contains("\"runtimeTracing\":false"))
        assertTrue(lines.any { it.contains("\"kind\":\"class\"") })
        assertTrue(lines.any { it.contains("\"kind\":\"edge\"") })

        task.analysisEnabled.set(false)
        task.merge()

        assertFalse(output.exists())
        assertFalse(shards.exists())
    }
}
