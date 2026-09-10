package io.jankhunter.buildlogic

import org.gradle.testfixtures.ProjectBuilder
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterCliPackagePluginTest {
    @Test
    fun appliesArchiveAndMavenPublishingInfrastructure() {
        val project = ProjectBuilder.builder().build()

        JankHunterCliPackagePlugin().apply(project)

        assertTrue(project.pluginManager.hasPlugin("base"))
        assertTrue(project.pluginManager.hasPlugin("maven-publish"))
        assertTrue(project.pluginManager.hasPlugin("signing"))
    }
}
