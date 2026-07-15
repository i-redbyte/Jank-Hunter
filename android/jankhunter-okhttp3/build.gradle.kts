import org.gradle.api.publish.PublishingExtension
import org.gradle.api.publish.maven.MavenPublication

plugins {
    id("com.android.library")
    id("maven-publish")
}

android {
    namespace = "io.jankhunter.okhttp3"
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
    implementation(project(":jankhunter-runtime"))
    compileOnly("com.squareup.okhttp3:okhttp:3.12.13")
    testImplementation("com.squareup.okhttp3:okhttp:3.12.13")
    testImplementation("junit:junit:4.13.2")
}

afterEvaluate {
    extensions.configure<PublishingExtension>("publishing") {
        publications {
            create<MavenPublication>("release") {
                from(components["release"])
                artifactId = "jankhunter-okhttp3"
            }
        }
    }
}
