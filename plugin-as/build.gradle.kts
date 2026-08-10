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
    testImplementation("junit:junit:4.13.2")
    testImplementation("org.opentest4j:opentest4j:1.3.0")

    intellijPlatform {
        val localIde = providers.gradleProperty("localIdePath").orNull
            ?.trim()
            ?.takeIf { it.isNotEmpty() && file(it).exists() }

        if (localIde != null) {
            local(localIde)
        } else {
            intellijIdea(providers.gradleProperty("platformVersion").get())
        }
        testFramework(TestFrameworkType.Bundled)
        pluginVerifier("1.408")
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
    instrumentCode = false

    pluginConfiguration {
        id = providers.gradleProperty("pluginId")
        name = providers.gradleProperty("pluginName")
        version = providers.gradleProperty("pluginVersion")

        description = """
            <p><b>Jank Hunter for Android</b> brings Android jank investigation into Android Studio and IntelliJ IDEA.</p>
            <p>Run the local Jank Hunter CLI, validate <code>.jhlog</code> inputs, inspect or compare captures,
            add optional HPROF evidence and Android Gradle Plugin artifacts, and open generated HTML reports.</p>
        """.trimIndent()

        changeNotes = """
            <ul>
              <li>Simple inspect and compare workflows in a modeless window.</li>
              <li>Optional advanced artifacts, filters, report appearance, scorecard, and CLI diagnostics.</li>
            </ul>
        """.trimIndent()

        ideaVersion {
            sinceBuild = providers.gradleProperty("pluginSinceBuild")
        }

        vendor {
            name = providers.gradleProperty("pluginVendor")
        }
    }

    pluginVerification {
        freeArgs.add("-offline")
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
