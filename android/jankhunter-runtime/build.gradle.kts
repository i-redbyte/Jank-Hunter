plugins {
    id("io.jankhunter.android-library")
    id("io.jankhunter.sql-normalizer-sources")
}

android {
    namespace = "io.jankhunter.runtime"

    defaultConfig {
        consumerProguardFiles("consumer-rules.pro")
    }

    sourceSets.getByName("test").kotlin.directories.add("src/sharedTest/kotlin")
    sourceSets.getByName("androidTest").kotlin.directories.add("src/sharedTest/kotlin")

    testOptions {
        unitTests.isReturnDefaultValues = true
    }
}

dependencies {
    implementation(libs.androidx.core)
    implementation(libs.androidx.metrics.performance)

    androidTestImplementation(libs.bundles.androidx.test)
    testImplementation(libs.junit)
    testImplementation(libs.androidx.work.runtime)
}

apply(from = rootProject.file("gradle/runtime-benchmarks.gradle.kts"))
