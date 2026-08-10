plugins {
    id("io.jankhunter.android-library")
}

android {
    namespace = "io.jankhunter.runtime"

    defaultConfig {
        consumerProguardFiles("consumer-rules.pro")
    }

    testOptions {
        unitTests.isReturnDefaultValues = true
    }
}

dependencies {
    androidTestImplementation(libs.bundles.androidx.test)
    testImplementation(libs.junit)
}

apply(from = rootProject.file("gradle/runtime-benchmarks.gradle.kts"))
