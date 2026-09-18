package io.jankhunter.buildlogic

import org.gradle.testfixtures.ProjectBuilder
import org.junit.Assert.assertTrue
import org.junit.Assert.assertThrows
import org.junit.Test

class JankHunterCliPackagePluginTest {
    @Test
    fun archiveRejectsMissingOrEmptyRetraceFiles() {
        val project = ProjectBuilder.builder().build()
        val task = project.tasks.register("archive", JankHunterCliArchiveTask::class.java).get()
        val directory = project.file("retrace").apply { mkdirs() }
        task.retraceDirectory.set(directory)
        assertThrows(IllegalStateException::class.java) { task.validateRetraceBundle() }
        val names = listOf("r8lib-9.0.32.jar", "jankhunter-retrace.jar", "R8-LICENSE.txt", "NOTICE.txt")
        names.forEach { directory.resolve(it).writeText("fixture") }
        task.validateRetraceBundle()
        for (name in names) {
            directory.resolve(name).writeText("")
            assertThrows(IllegalStateException::class.java) { task.validateRetraceBundle() }
            directory.resolve(name).writeText("fixture")
        }
    }

    @Test
    fun appliesArchiveAndMavenPublishingInfrastructure() {
        val project = ProjectBuilder.builder().build()

        JankHunterCliPackagePlugin().apply(project)

        assertTrue(project.pluginManager.hasPlugin("base"))
        assertTrue(project.pluginManager.hasPlugin("maven-publish"))
        assertTrue(project.pluginManager.hasPlugin("signing"))
    }
}
