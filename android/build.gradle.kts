import io.gitlab.arturbosch.detekt.Detekt
import io.gitlab.arturbosch.detekt.extensions.DetektExtension
import org.gradle.api.artifacts.repositories.PasswordCredentials
import org.gradle.api.publish.PublishingExtension
import org.gradle.api.publish.maven.MavenPublication
import org.gradle.plugins.signing.SigningExtension

plugins {
    id("com.android.library") version "9.0.1" apply false
    id("com.android.application") version "9.0.1" apply false
    id("org.jetbrains.kotlin.jvm") version "2.3.20" apply false
    id("io.gitlab.arturbosch.detekt") version "1.23.8" apply false
}

val detektVersion = "1.23.8"

allprojects {
    group = providers.gradleProperty("jankHunterGroup").get()
    version = providers.gradleProperty("jankHunterVersion").get()
    description = when (name) {
        "jankhunter-runtime" -> "Dependency-light Android runtime for local jank, network, memory, and leak diagnostics."
        "jankhunter-annotations" -> "Dependency-light annotations for Jank Hunter attribution and instrumentation control."
        "jankhunter-okhttp3" -> "Optional OkHttp 3 integration for Jank Hunter network telemetry."
        "jankhunter-android-sdk" -> "Single Android SDK dependency for Jank Hunter runtime, annotations, and OkHttp helpers."
        "jankhunter-gradle-plugin" -> "Gradle/ASM instrumentation plugin for Jank Hunter Android builds."
        else -> "Jank Hunter Android component."
    }
}

subprojects {
    apply(plugin = "io.gitlab.arturbosch.detekt")

    dependencies {
        add("detektPlugins", "io.gitlab.arturbosch.detekt:detekt-formatting:$detektVersion")
    }

    extensions.configure<DetektExtension>("detekt") {
        buildUponDefaultConfig = false
        allRules = false
        autoCorrect = false
        config.setFrom(rootProject.files("config/detekt/detekt.yml"))
        basePath = rootDir.absolutePath
    }

    tasks.withType<Detekt>().configureEach {
        jvmTarget = "17"
        reports {
            html.required.set(true)
            xml.required.set(true)
            sarif.required.set(true)
            md.required.set(false)
        }
    }

    plugins.withId("maven-publish") {
        apply(plugin = "signing")

        afterEvaluate {
            extensions.configure<PublishingExtension>("publishing") {
                repositories {
                    maven {
                        name = "GitHubPackages"
                        url = uri("https://maven.pkg.github.com/i-redbyte/Jank-Hunter")
                        credentials(PasswordCredentials::class) {
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
                                credentials(PasswordCredentials::class) {
                                    username = providers.environmentVariable("MAVEN_REPOSITORY_USERNAME").orNull ?: ""
                                    password = providers.environmentVariable("MAVEN_REPOSITORY_PASSWORD").orNull ?: ""
                                }
                            }
                        }
                    }
                }

                publications.withType<MavenPublication>().configureEach {
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
                }
            }

            extensions.configure<SigningExtension>("signing") {
                isRequired = providers.environmentVariable("JANKHUNTER_SIGNING_REQUIRED")
                    .map(String::toBoolean)
                    .getOrElse(false)
                val signingKey = providers.environmentVariable("JANKHUNTER_SIGNING_KEY")
                    .orElse(providers.gradleProperty("signingInMemoryKey"))
                    .orNull
                val signingPassword = providers.environmentVariable("JANKHUNTER_SIGNING_PASSWORD")
                    .orElse(providers.gradleProperty("signingInMemoryKeyPassword"))
                    .orNull
                if (!signingKey.isNullOrBlank() && !signingPassword.isNullOrBlank()) {
                    useInMemoryPgpKeys(signingKey, signingPassword)
                    sign(extensions.getByType<PublishingExtension>().publications)
                }
            }
        }
    }
}
