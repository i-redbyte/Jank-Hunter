plugins {
    id("io.jankhunter.android-library")
    id("io.jankhunter.sql-normalizer-sources")
}

android {
    namespace = "io.jankhunter.runtime"

    testOptions {
        unitTests.isReturnDefaultValues = true
    }
}

dependencies {
    implementation(libs.androidx.core)
    implementation(libs.androidx.metrics.performance)

    androidTestImplementation(libs.bundles.androidx.test)
    testImplementation(libs.junit)
}

apply(from = rootProject.file("gradle/runtime-benchmarks.gradle.kts"))
