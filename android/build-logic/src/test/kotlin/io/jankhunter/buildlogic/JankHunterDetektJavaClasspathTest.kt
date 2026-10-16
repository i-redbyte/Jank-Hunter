package io.jankhunter.buildlogic

import dev.detekt.gradle.Detekt
import org.gradle.api.internal.project.ProjectInternal
import org.gradle.api.tasks.compile.JavaCompile
import org.gradle.testfixtures.ProjectBuilder
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterDetektJavaClasspathTest {
    @Test
    fun typedAnalysisIncludesMatchingJavaOutputsAndTheirProducer() {
        val project = ProjectBuilder.builder().build()
        project.configureDetektJavaClasspath()
        val release = project.tasks.register("detektRelease", Detekt::class.java)
        val unitTest = project.tasks.register("detektDemoReleaseUnitTest", Detekt::class.java)
        val releaseJava = project.tasks.register("compileReleaseJavaWithJavac", JavaCompile::class.java) {
            destinationDirectory.set(project.layout.buildDirectory.dir("release-java"))
        }
        val unitTestJava = project.tasks.register("compileDemoReleaseUnitTestJavaWithJavac", JavaCompile::class.java) {
            destinationDirectory.set(project.layout.buildDirectory.dir("test-java"))
        }
        (project as ProjectInternal).evaluate()

        assertEquals(setOf(releaseJava.get().destinationDirectory.get().asFile), release.get().classpath.files)
        assertEquals(setOf(unitTestJava.get().destinationDirectory.get().asFile), unitTest.get().classpath.files)
        assertTrue(release.get().taskDependencies.getDependencies(release.get()).contains(releaseJava.get()))
        assertTrue(unitTest.get().taskDependencies.getDependencies(unitTest.get()).contains(unitTestJava.get()))
        assertFalse(release.get().taskDependencies.getDependencies(release.get()).contains(unitTestJava.get()))
    }

    @Test
    fun syntaxOnlyAndJvmTasksPreserveTheirClasspathWithoutAndroidJavaCompilation() {
        val project = ProjectBuilder.builder().build()
        project.configureDetektJavaClasspath()
        val dependency = project.file("dependency.jar")
        val syntax = project.tasks.register("detekt", Detekt::class.java)
        val jvm = project.tasks.register("detektMain", Detekt::class.java) {
            classpath.from(dependency)
        }
        val java = project.tasks.register("compileJava", JavaCompile::class.java)
        (project as ProjectInternal).evaluate()

        assertTrue(syntax.get().classpath.isEmpty)
        assertEquals(setOf(dependency), jvm.get().classpath.files)
        assertFalse(jvm.get().taskDependencies.getDependencies(jvm.get()).contains(java.get()))
    }
}
