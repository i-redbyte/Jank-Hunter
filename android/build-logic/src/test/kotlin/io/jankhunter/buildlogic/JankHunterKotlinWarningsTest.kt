package io.jankhunter.buildlogic

import org.gradle.testfixtures.ProjectBuilder
import org.jetbrains.kotlin.gradle.tasks.KotlinCompile
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterKotlinWarningsTest {
    @Test
    fun compilerWarningsFailEveryKotlinCompilation() {
        val project = ProjectBuilder.builder().build()
        project.pluginManager.apply("org.jetbrains.kotlin.jvm")

        project.configureKotlinWarnings()

        project.tasks.withType(KotlinCompile::class.java).forEach { task ->
            assertTrue(task.compilerOptions.allWarningsAsErrors.get())
        }
    }
}
