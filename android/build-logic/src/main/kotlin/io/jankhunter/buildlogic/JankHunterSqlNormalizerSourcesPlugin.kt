package io.jankhunter.buildlogic

import com.android.build.api.dsl.LibraryExtension
import java.io.File
import org.gradle.api.Plugin
import org.gradle.api.Project
import org.gradle.kotlin.dsl.configure
import org.jetbrains.kotlin.gradle.dsl.KotlinJvmProjectExtension

class JankHunterSqlNormalizerSourcesPlugin : Plugin<Project> {
    override fun apply(target: Project) = with(target) {
        val sources = locateSqlNormalizerSources(projectDir)
        pluginManager.withPlugin("org.jetbrains.kotlin.jvm") {
            extensions.configure<KotlinJvmProjectExtension> {
                sourceSets.named("main") { kotlin.srcDir(sources.main) }
                sourceSets.named("test") { kotlin.srcDir(sources.test) }
            }
        }
        pluginManager.withPlugin("com.android.library") {
            extensions.configure<LibraryExtension> {
                sourceSets.getByName("main").kotlin.directories.add(sources.main.path)
                sourceSets.getByName("test").kotlin.directories.add(sources.test.path)
            }
        }
    }
}

internal data class SqlNormalizerSources(
    val main: File,
    val test: File,
)

internal fun locateSqlNormalizerSources(start: File): SqlNormalizerSources {
    var candidate: File? = start.canonicalFile
    while (candidate != null) {
        val component = candidate.resolve(SQL_NORMALIZER_COMPONENT)
        val main = component.resolve(MAIN_SOURCES)
        if (main.isDirectory) {
            val test = component.resolve(TEST_SOURCES)
            check(test.isDirectory) { "Missing sql-normalizer test sources: $test" }
            return SqlNormalizerSources(main.canonicalFile, test.canonicalFile)
        }
        candidate = candidate.parentFile
    }
    error("Cannot locate $SQL_NORMALIZER_COMPONENT from ${start.canonicalPath}")
}

private const val SQL_NORMALIZER_COMPONENT = "sql-normalizer"
private const val MAIN_SOURCES = "src/main/kotlin"
private const val TEST_SOURCES = "src/test/kotlin"
