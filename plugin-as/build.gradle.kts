import org.jetbrains.kotlin.gradle.dsl.JvmTarget
import org.jetbrains.kotlin.gradle.tasks.KotlinCompile
import org.jetbrains.intellij.platform.gradle.TestFrameworkType

plugins {
    id("java")
    id("org.jetbrains.kotlin.jvm") version "2.3.20"
    id("org.jetbrains.intellij.platform") version "2.16.0"
}

group = providers.gradleProperty("pluginGroup").get()
version = providers.gradleProperty("pluginVersion").get()

repositories {
    mavenCentral()
    intellijPlatform {
        defaultRepositories()
    }
}

dependencies {
    implementation("com.google.code.gson:gson:2.11.0")
    testImplementation("junit:junit:4.13.2")

    intellijPlatform {
        val localIde = providers.gradleProperty("localIdePath").orNull
            ?.trim()
            ?.takeIf { it.isNotEmpty() && file(it).exists() }

        if (localIde != null) {
            local(localIde)
        } else {
            intellijIdea(providers.gradleProperty("platformVersion").get())
        }
        testFramework(TestFrameworkType.Platform)
    }
}

java {
    toolchain {
        languageVersion = JavaLanguageVersion.of(21)
    }
}

kotlin {
    jvmToolchain(21)
}

tasks.withType<KotlinCompile>().configureEach {
    compilerOptions {
        jvmTarget.set(JvmTarget.JVM_21)
    }
}

tasks.test {
    useJUnit()
}

intellijPlatform {
    buildSearchableOptions = false

    pluginConfiguration {
        id = providers.gradleProperty("pluginId")
        name = providers.gradleProperty("pluginName")
        version = providers.gradleProperty("pluginVersion")

        description = """
            <p>Runs the Jank Hunter CLI from Android Studio or IntelliJ IDEA, captures console output,
            and opens generated reports inside the IDE or in the system browser.</p>
        """.trimIndent()

        changeNotes = """
            <ul>
              <li>Initial tool window for inspect, compare, problems, scorecard, sample, and version commands.</li>
              <li>Settings page for CLI path and default report behavior.</li>
            </ul>
        """.trimIndent()

        ideaVersion {
            sinceBuild = providers.gradleProperty("pluginSinceBuild")
            untilBuild = providers.gradleProperty("pluginUntilBuild")
        }

        vendor {
            name = providers.gradleProperty("pluginVendor")
        }
    }

    pluginVerification {
        ides {
            val localIde = providers.gradleProperty("localIdePath").orNull
                ?.trim()
                ?.takeIf { it.isNotEmpty() && file(it).exists() }

            if (localIde != null) {
                local(file(localIde))
            } else {
                current()
            }
        }
    }
}
