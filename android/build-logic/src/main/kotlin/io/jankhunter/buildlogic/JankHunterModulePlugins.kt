package io.jankhunter.buildlogic

import com.android.build.api.dsl.ApplicationExtension
import com.android.build.api.dsl.LibraryExtension
import dev.detekt.gradle.Detekt
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
import org.jetbrains.kotlin.buildtools.api.ExperimentalBuildToolsApi
import org.jetbrains.kotlin.gradle.ExperimentalKotlinGradlePluginApi
import org.jetbrains.kotlin.gradle.dsl.JvmTarget
import org.jetbrains.kotlin.gradle.dsl.KotlinBaseExtension
import org.jetbrains.kotlin.gradle.dsl.KotlinVersion
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
            freeCompilerArgs.add("-Xjdk-release=17")
        }
    }
    configureKotlinWarnings()
}

internal fun Project.configureKotlinWarnings() {
    tasks.withType<KotlinCompile>().configureEach {
        compilerOptions.allWarningsAsErrors.set(true)
    }
}

/** Device publication is independent of host tooling and application compiler versions. */
@OptIn(ExperimentalBuildToolsApi::class, ExperimentalKotlinGradlePluginApi::class)
internal fun Project.configureDeviceKotlinBoundary() {
    configurePortableCompiler(JvmTarget.JVM_1_8)
    extensions.findByType(JavaPluginExtension::class.java)?.apply {
        sourceCompatibility = JavaVersion.VERSION_1_8
        targetCompatibility = JavaVersion.VERSION_1_8
    }
}

@OptIn(ExperimentalBuildToolsApi::class, ExperimentalKotlinGradlePluginApi::class)
private fun Project.configurePortableCompiler(target: JvmTarget, compiler: String = "2.0.21") {
    extensions.configure<KotlinBaseExtension> {
        compilerVersion.set(compiler)
        coreLibrariesVersion = "1.9.25"
    }
    tasks.withType<KotlinCompile>().configureEach {
        compilerOptions {
            languageVersion.set(KotlinVersion.KOTLIN_1_9)
            apiVersion.set(KotlinVersion.KOTLIN_1_9)
            jvmTarget.set(target)
            freeCompilerArgs.add("-Xjvm-default=all-compatibility")
        }
    }
    // Detekt's host compiler no longer accepts 1.9; device compilation keeps its 1.9 API floor.
    tasks.withType<Detekt>().configureEach {
        languageVersion.set("2.0")
        apiVersion.set("2.0")
    }
    dependencies.add("api", "org.jetbrains.kotlin:kotlin-stdlib:1.9.25")
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
        configureDeviceKotlinBoundary()

        extensions.configure<LibraryExtension> {
            compileOptions {
                sourceCompatibility = JavaVersion.VERSION_1_8
                targetCompatibility = JavaVersion.VERSION_1_8
            }
            compileSdk = catalogVersion("android-compile-sdk").toInt()
            configureBuildTools { buildToolsVersion = it }
            defaultConfig {
                minSdk = catalogVersion("android-min-sdk").toInt()
                aarMetadata {
                    minCompileSdk = if (project.name == "jankhunter-jankstats") 35 else 33
                }
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

class JankHunterMetadataReaderPlugin : Plugin<Project> {
    override fun apply(target: Project) = with(target) {
        pluginManager.apply("org.jetbrains.kotlin.jvm")
        configureJvm17()
        configureJankHunterDetekt()
    }
}

class JankHunterKotlinLibraryPlugin : Plugin<Project> {
    override fun apply(target: Project) = with(target) {
        pluginManager.apply("org.jetbrains.kotlin.jvm")
        pluginManager.apply("maven-publish")
        configureJankHunterDetekt()
        configureJankHunterPublishing()
        configureKotlinWarnings()
        configureDeviceKotlinBoundary()
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
        configurePortableCompiler(JvmTarget.JVM_17, "2.1.21")
    }
}

class JankHunterCliPackagePlugin : Plugin<Project> {
    override fun apply(target: Project) = with(target) {
        pluginManager.apply("base")
        pluginManager.apply("maven-publish")
        configureJankHunterPublishing()
    }
}
