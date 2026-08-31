plugins {
    alias(libs.plugins.ksp)
    id("io.jankhunter.android-application")
    id("io.jankhunter.android")
}

android {
    namespace = "io.jankhunter.sample"

    defaultConfig {
        applicationId = "io.jankhunter.sample"
        versionCode = 1
    }
}

jankHunter {
    enabled.set(true)
    enabledBuildTypes.set(setOf("debug"))
    profile.set(io.jankhunter.gradle.JankHunterProfile.FULL)
    packages("io.jankhunter.sample.graph")
    storageLimitMiB(50)
    debug {
        enable(
            io.jankhunter.gradle.JankHunterFeature.HEAP_DUMPS,
            io.jankhunter.gradle.JankHunterFeature.MAIN_LOOPER,
            io.jankhunter.gradle.JankHunterFeature.METHOD_COUNTERS,
        )
        privacyReviewed()
    }

    autoInit.set(true)
    tuning.thresholds.mainThreadStallMs.set(150)
    tuning.thresholds.ownerBlockMs.set(100)
    tuning.thresholds.slowHttpMs.set(500)
    tuning.heapDumps.minIntervalMs.set(1_000)
    tuning.heapDumps.minRetainedAgeMs.set(1_000)
}

dependencies {
    implementation(project(":jankhunter-runtime"))
    implementation(project(":jankhunter-okhttp3"))
    implementation(libs.okhttp)
    implementation(libs.androidx.room.runtime)
    ksp("androidx.room:room-compiler:${libs.versions.room.get()}")
    implementation(libs.androidx.core)
    implementation(libs.androidx.activity.compose)
    implementation(libs.bundles.androidx.lifecycle)
    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.bundles.androidx.compose)

    debugImplementation(libs.leakcanary)
    debugImplementation(libs.androidx.compose.ui.tooling)

    androidTestImplementation(libs.bundles.androidx.test)
    testImplementation(libs.junit)
}
