import org.gradle.api.publish.PublishingExtension
import org.gradle.api.publish.maven.MavenPublication
import org.gradle.api.tasks.testing.Test

plugins {
    id("com.android.library")
    id("maven-publish")
}

android {
    namespace = "io.jankhunter.runtime"
    compileSdk = 35
    providers.gradleProperty("jankHunterBuildToolsVersion").orNull?.let {
        buildToolsVersion = it
    }

    defaultConfig {
        minSdk = 23
        consumerProguardFiles("consumer-rules.pro")
    }

    publishing {
        singleVariant("release") {
            withSourcesJar()
        }
    }
}

dependencies {
    testImplementation("junit:junit:4.13.2")
}

tasks.withType<Test>().configureEach {
    systemProperty("jankhunter.benchmark", providers.systemProperty("jankhunter.benchmark").orElse("false").get())
    systemProperty(
        "jankhunter.benchmark.iterations",
        providers.systemProperty("jankhunter.benchmark.iterations").orElse("100000").get(),
    )
}

afterEvaluate {
    extensions.configure<PublishingExtension>("publishing") {
        publications {
            create<MavenPublication>("release") {
                from(components["release"])
                artifactId = "jankhunter-runtime"
            }
        }
    }
}
