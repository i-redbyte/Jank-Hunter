import org.gradle.api.artifacts.repositories.PasswordCredentials
import org.gradle.api.publish.PublishingExtension
import org.gradle.api.publish.maven.MavenPublication
import org.gradle.plugins.signing.SigningExtension

plugins {
    id("com.android.library") version "9.0.1" apply false
    id("com.android.application") version "9.0.1" apply false
    id("org.jetbrains.kotlin.jvm") version "2.3.20" apply false
}

allprojects {
    group = providers.gradleProperty("jankHunterGroup").get()
    version = providers.gradleProperty("jankHunterVersion").get()
    description = when (name) {
        "jankhunter-runtime" -> "Dependency-light Android runtime for local jank, network, memory, and leak diagnostics."
        "jankhunter-okhttp3" -> "Optional OkHttp 3 integration for Jank Hunter network telemetry."
        "jankhunter-gradle-plugin" -> "Gradle/ASM instrumentation plugin for Jank Hunter Android builds."
        else -> "Jank Hunter Android component."
    }
}

subprojects {
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
                            credentials(PasswordCredentials::class) {
                                username = providers.environmentVariable("MAVEN_REPOSITORY_USERNAME").orNull ?: ""
                                password = providers.environmentVariable("MAVEN_REPOSITORY_PASSWORD").orNull ?: ""
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
