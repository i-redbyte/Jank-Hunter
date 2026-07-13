import org.gradle.api.publish.PublishingExtension
import org.gradle.api.publish.maven.MavenPublication

plugins {
    id("com.android.library")
    id("maven-publish")
}

android {
    namespace = "io.jankhunter.sdk"
    compileSdk = 35
    providers.gradleProperty("jankHunterBuildToolsVersion").orNull?.let {
        buildToolsVersion = it
    }

    defaultConfig {
        minSdk = 23
    }

    publishing {
        singleVariant("release") {
            withSourcesJar()
        }
    }
}

dependencies {
    api(project(":jankhunter-annotations"))
    api(project(":jankhunter-runtime"))
    api(project(":jankhunter-okhttp3"))
}

afterEvaluate {
    extensions.configure<PublishingExtension>("publishing") {
        publications {
            create<MavenPublication>("release") {
                from(components["release"])
                artifactId = "jankhunter-android-sdk"
            }
        }
    }
}
