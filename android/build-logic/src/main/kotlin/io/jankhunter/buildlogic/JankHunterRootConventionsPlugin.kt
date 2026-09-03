package io.jankhunter.buildlogic

import dev.detekt.gradle.Detekt
import dev.detekt.gradle.extensions.DetektExtension
import org.gradle.api.Plugin
import org.gradle.api.Project
import org.gradle.api.artifacts.VersionCatalogsExtension
import org.gradle.api.artifacts.repositories.PasswordCredentials
import org.gradle.api.publish.PublishingExtension
import org.gradle.api.publish.maven.MavenPublication
import org.gradle.kotlin.dsl.configure
import org.gradle.kotlin.dsl.getByType
import org.gradle.kotlin.dsl.withType
import org.gradle.plugins.signing.SigningExtension

class JankHunterRootConventionsPlugin : Plugin<Project> {
    override fun apply(target: Project) = with(target) {
        require(this == rootProject)
        configureProjectMetadata()
        configureAllKotlinImportAnalysis()
    }

    private fun Project.configureProjectMetadata() {
        allprojects {
            group = providers.gradleProperty("jankHunterGroup").get()
            version = providers.gradleProperty("jankHunterVersion").get()
            description = when (name) {
                "jankhunter-runtime" -> {
                    "Dependency-light Android runtime for local jank, network, memory, and leak diagnostics."
                }
                "jankhunter-annotations" -> {
                    "Dependency-light annotations for Jank Hunter attribution and instrumentation control."
                }
                "jankhunter-okhttp3" -> "Optional OkHttp 3 integration for Jank Hunter network telemetry."
                "jankhunter-workmanager" -> "Optional WorkManager lifecycle integration for Jank Hunter."
                "jankhunter-gradle-plugin" -> "Gradle/ASM instrumentation plugin for Jank Hunter Android builds."
                else -> "Jank Hunter Android component."
            }
        }
    }

}

private fun Project.configureAllKotlinImportAnalysis() {
    configureJankHunterDetekt()
    val repositoryRoot = projectDir.parentFile
    tasks.named("detekt", Detekt::class.java) {
        description = "Checks imports in every Jank Hunter Kotlin source set and build script."
        setSource(
            fileTree(repositoryRoot) {
                include("android/**/*.kt", "android/**/*.kts", "plugin-as/**/*.kt", "plugin-as/**/*.kts")
                exclude("**/build/**", "**/.gradle/**", "**/.kotlin/**", "**/.intellijPlatform/**")
            },
        )
    }
}

internal fun Project.configureJankHunterDetekt() {
    pluginManager.apply("dev.detekt")
    val catalog = rootProject.extensions.getByType<VersionCatalogsExtension>().named("libs")
    dependencies.add("detektPlugins", catalog.findLibrary("detekt-rules-ktlint-wrapper").get())
    extensions.configure<DetektExtension> {
        buildUponDefaultConfig.set(false)
        allRules.set(false)
        autoCorrect.set(false)
        config.setFrom(files(jankHunterWorkspaceFile("config/detekt/detekt.yml")))
        basePath.set(jankHunterWorkspaceFile(""))
    }
    tasks.withType<Detekt>().configureEach {
        jvmTarget.set("17")
        reports {
            html.required.set(true)
            checkstyle.required.set(true)
            sarif.required.set(true)
            markdown.required.set(false)
        }
    }
}

internal fun Project.configureJankHunterPublishing() {
    pluginManager.apply("signing")
    afterEvaluate {
        val publishing = extensions.getByType<PublishingExtension>()
        publishing.repositories {
            maven {
                name = "GitHubPackages"
                url = uri("https://maven.pkg.github.com/i-redbyte/Jank-Hunter")
                credentials(PasswordCredentials::class.java) {
                    username = providers.environmentVariable("GITHUB_ACTOR")
                        .orElse(providers.gradleProperty("gpr.user"))
                        .orNull
                        ?: ""
                    password = providers.environmentVariable("GITHUB_TOKEN")
                        .orElse(providers.gradleProperty("gpr.key"))
                        .orNull
                        ?: ""
                }
            }

            val releaseRepositoryUrl = providers.environmentVariable("MAVEN_REPOSITORY_URL").orNull
            if (!releaseRepositoryUrl.isNullOrBlank()) {
                maven {
                    name = "RemoteRelease"
                    url = uri(releaseRepositoryUrl)
                    if (!releaseRepositoryUrl.startsWith("file:")) {
                        credentials(PasswordCredentials::class.java) {
                            username = providers.environmentVariable("MAVEN_REPOSITORY_USERNAME").orNull ?: ""
                            password = providers.environmentVariable("MAVEN_REPOSITORY_PASSWORD").orNull ?: ""
                        }
                    }
                }
            }
        }

        val signing = extensions.getByType<SigningExtension>()
        signing.isRequired = providers.environmentVariable("JANKHUNTER_SIGNING_REQUIRED")
            .map(String::toBoolean)
            .getOrElse(false)
        val signingKey = providers.environmentVariable("JANKHUNTER_SIGNING_KEY")
            .orElse(providers.gradleProperty("signingInMemoryKey"))
            .orNull
        val signingPassword = providers.environmentVariable("JANKHUNTER_SIGNING_PASSWORD")
            .orElse(providers.gradleProperty("signingInMemoryKeyPassword"))
            .orNull
        if (!signingKey.isNullOrBlank() && !signingPassword.isNullOrBlank()) {
            signing.useInMemoryPgpKeys(signingKey, signingPassword)
        }

        publishing.publications.withType<MavenPublication>().configureEach {
            pom {
                name.set("Jank Hunter ${project.name.removePrefix("jankhunter-")}")
                description.set(project.description)
                url.set("https://github.com/i-redbyte/Jank-Hunter")
                licenses {
                    license {
                        name.set("Apache License 2.0")
                        url.set("https://www.apache.org/licenses/LICENSE-2.0.txt")
                    }
                }
                developers {
                    developer {
                        id.set("i-redbyte")
                        name.set("i-redbyte")
                    }
                }
                scm {
                    connection.set("scm:git:git://github.com/i-redbyte/Jank-Hunter.git")
                    developerConnection.set("scm:git:ssh://git@github.com/i-redbyte/Jank-Hunter.git")
                    url.set("https://github.com/i-redbyte/Jank-Hunter")
                }
            }
            if (!signingKey.isNullOrBlank() && !signingPassword.isNullOrBlank()) {
                signing.sign(this)
            }
        }
    }
}

private fun Project.jankHunterWorkspaceFile(relativePath: String): java.io.File {
    val currentRoot = rootProject.projectDir
    if (java.io.File(currentRoot, "config/detekt/detekt.yml").isFile) {
        return java.io.File(currentRoot, relativePath)
    }
    return java.io.File(currentRoot.parentFile, relativePath)
}
