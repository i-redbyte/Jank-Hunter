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
        // The selected local IDE already ships the matching test framework. Keeping it bundled
        // avoids a redundant remote artifact lookup and guarantees ABI parity with that IDE.
        testFramework(TestFrameworkType.Bundled)
        // Pin the verifier so Gradle can resolve the executable deterministically from Maven
        // Central without querying JetBrains' latest-version endpoint during every build.
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
    // The plugin builds its Swing UI in Kotlin and has no IntelliJ GUI Designer .form files.
    // Bytecode instrumentation would only add a remote java-compiler dependency and build work.
    instrumentCode = false

    pluginConfiguration {
        id = providers.gradleProperty("pluginId")
        name = providers.gradleProperty("pluginName")
        version = providers.gradleProperty("pluginVersion")

        description = """
            <p><b>Jank Hunter for Android</b> brings Android jank investigation into Android Studio and IntelliJ IDEA.</p>
            <p>Run the local Jank Hunter CLI, validate <code>.jhlog</code> inputs, discover Android Gradle Plugin artifacts,
            collect logs from connected devices, inspect or compare captures, open HTML reports, and review detected
            problems with source navigation directly in the IDE.</p>
        """.trimIndent()

        changeNotes = """
            <ul>
              <li>Initial tool window for inspect, compare, problems, scorecard, sample, and version commands.</li>
              <li>Settings page for CLI path and default report behavior.</li>
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
        // Jank Hunter has no Marketplace plugin dependencies. Offline verification keeps the
        // result deterministic and prevents a local/CI network outage from masking ABI checks.
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
