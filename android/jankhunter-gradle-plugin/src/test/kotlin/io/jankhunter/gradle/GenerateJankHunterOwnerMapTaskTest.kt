package io.jankhunter.gradle

import org.gradle.testfixtures.ProjectBuilder
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class GenerateJankHunterOwnerMapTaskTest {
    @Test
    fun writesMetadataJsonlWithoutFakeOwnersObject() {
        val project = ProjectBuilder.builder().build()
        val task = project.tasks.register(
            "generateOwnerMap",
            GenerateJankHunterOwnerMapTask::class.java,
        ).get()
        task.outputFile.set(project.layout.buildDirectory.file("jankhunter/owner-map.json"))
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
}
