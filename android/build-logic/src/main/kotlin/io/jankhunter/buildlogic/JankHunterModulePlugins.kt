package io.jankhunter.buildlogic

import com.android.build.api.dsl.ApplicationExtension
import com.android.build.api.dsl.LibraryExtension
import org.gradle.api.JavaVersion
import org.gradle.api.Plugin
import org.gradle.api.Project
import org.gradle.api.artifacts.VersionCatalogsExtension
import org.gradle.api.plugins.JavaPluginExtension
import org.gradle.api.publish.PublishingExtension
import org.gradle.api.publish.maven.MavenPublication
import org.gradle.api.tasks.compile.JavaCompile
import org.gradle.kotlin.dsl.configure
import org.gradle.kotlin.dsl.create
import org.gradle.kotlin.dsl.getByType
import org.gradle.kotlin.dsl.withType
import org.jetbrains.kotlin.gradle.dsl.JvmTarget
import org.jetbrains.kotlin.gradle.tasks.KotlinCompile

internal fun Project.catalogVersion(name: String): String {
    return extensions.getByType<VersionCatalogsExtension>()
        .named("libs")
        .findVersion(name)
        .get()
        .requiredVersion
}

internal fun Project.configureJvm17() {
    extensions.configure<JavaPluginExtension> {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    tasks.withType<JavaCompile>().configureEach {
        options.release.set(17)
    }
    tasks.withType<KotlinCompile>().configureEach {
        compilerOptions {
            jvmTarget.set(JvmTarget.JVM_17)
        }
    }
    configureKotlinWarnings()
}

internal fun Project.configureKotlinWarnings() {
    tasks.withType<KotlinCompile>().configureEach {
        compilerOptions.allWarningsAsErrors.set(true)
    }
}

internal fun Project.configureBuildTools(setVersion: (String) -> Unit) {
    providers.gradleProperty("jankHunterBuildToolsVersion").orNull?.let(setVersion)
}

class JankHunterAndroidLibraryPlugin : Plugin<Project> {
    override fun apply(target: Project) = with(target) {
        pluginManager.apply("com.android.library")
        pluginManager.apply("maven-publish")
        configureJankHunterDetekt()
        configureJankHunterPublishing()
        configureKotlinWarnings()

        extensions.configure<LibraryExtension> {
            compileSdk = catalogVersion("android-compile-sdk").toInt()
            configureBuildTools { buildToolsVersion = it }
            defaultConfig {
                minSdk = catalogVersion("android-min-sdk").toInt()
            }
            publishing {
                singleVariant("release") {
                    withSourcesJar()
                }
            }
        }

        afterEvaluate {
            extensions.configure<PublishingExtension> {
                publications {
                    if (findByName("release") == null) {
                        create<MavenPublication>("release") {
                            from(components.getByName("release"))
                            artifactId = project.name
                        }
                    }
                }
            }
        }
    }
}

class JankHunterAndroidApplicationPlugin : Plugin<Project> {
    override fun apply(target: Project) = with(target) {
        pluginManager.apply("com.android.application")
        pluginManager.apply("org.jetbrains.kotlin.plugin.compose")
        configureJankHunterDetekt()
        configureKotlinWarnings()

        extensions.configure<ApplicationExtension> {
            compileSdk = catalogVersion("android-compile-sdk").toInt()
            configureBuildTools { buildToolsVersion = it }
            defaultConfig {
                minSdk = catalogVersion("android-min-sdk").toInt()
                targetSdk = catalogVersion("android-target-sdk").toInt()
                versionName = providers.gradleProperty("jankHunterVersion").get().removeSuffix("-SNAPSHOT")
                testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
            }
            buildFeatures {
                compose = true
            }
        }
    }
}

class JankHunterKotlinLibraryPlugin : Plugin<Project> {
    override fun apply(target: Project) = with(target) {
        pluginManager.apply("org.jetbrains.kotlin.jvm")
        pluginManager.apply("maven-publish")
        configureJankHunterDetekt()
        configureJankHunterPublishing()
        configureJvm17()
        extensions.configure<JavaPluginExtension> {
            withSourcesJar()
        }

        afterEvaluate {
            extensions.configure<PublishingExtension> {
                publications {
                    if (findByName("release") == null) {
                        create<MavenPublication>("release") {
                            from(components.getByName("java"))
                            artifactId = project.name
                        }
                    }
                }
            }
        }
    }
}

class JankHunterKotlinGradlePlugin : Plugin<Project> {
    override fun apply(target: Project) = with(target) {
        pluginManager.apply("org.jetbrains.kotlin.jvm")
        pluginManager.apply("maven-publish")
        configureJankHunterDetekt()
        configureJankHunterPublishing()
        configureJvm17()
    }
}
