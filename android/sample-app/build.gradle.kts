plugins {
    alias(libs.plugins.ksp)
    id("io.jankhunter.android-application")
    id("io.jankhunter.android")
}

val benchmarkInPlaceOnly = providers.gradleProperty("jankHunterBenchmarkInPlaceOnly").map(String::toBoolean).orElse(false)

android {
    namespace = "io.jankhunter.sample"
    testBuildType = providers.gradleProperty("jankHunterTestBuildType").getOrElse("debug")

    defaultConfig {
        applicationId = "io.jankhunter.sample"
        versionCode = 1
        manifestPlaceholders["jankHunterBenchmarkInPlaceOnly"] = benchmarkInPlaceOnly.get()
    }
    buildTypes {
        release {
            applicationIdSuffix = ".release"
            isMinifyEnabled = true
            signingConfig = signingConfigs.getByName("debug")
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "concurrency-test-entrypoints.pro")
            testProguardFiles("concurrency-android-test.pro")
            if (benchmarkInPlaceOnly.get()) proguardFiles("observation-in-place-benchmark.pro")
        }
    }
}

jankHunter {
    enabled.set(true)
    enabledBuildTypes.set(setOf("debug", "release"))
    profile.set(io.jankhunter.gradle.JankHunterProfile.FULL)
    packages("io.jankhunter.sample.graph", "io.jankhunter.sample.concurrency")
    excludePackages("io.jankhunter.sample.benchmarkcontrol")
    storageLimitMiB(350)
    debug {
        enable(
            io.jankhunter.gradle.JankHunterFeature.HEAP_DUMPS,
            io.jankhunter.gradle.JankHunterFeature.MAIN_LOOPER,
            io.jankhunter.gradle.JankHunterFeature.METHOD_COUNTERS,
            io.jankhunter.gradle.JankHunterFeature.THREAD_SAMPLING,
            io.jankhunter.gradle.JankHunterFeature.CONCURRENCY_DIAGNOSTICS,
        )
        privacyReviewed()
    }
    release {
        profile.set(io.jankhunter.gradle.JankHunterProfile.TARGETED)
        processes.set(io.jankhunter.gradle.JankHunterProcesses.MAIN_ONLY)
        enable(
            io.jankhunter.gradle.JankHunterFeatureBundle.CONCURRENCY_ALL,
            io.jankhunter.gradle.JankHunterFeature.BYTECODE_IO,
            io.jankhunter.gradle.JankHunterFeature.RUNTIME_IO,
        )
        privacyReviewed()
        performanceBudget(project.file("concurrency-release-budget.txt"))
    }

    autoInit.set(providers.gradleProperty("jankHunterAutoInit").map(String::toBoolean).orElse(true))
    tuning.thresholds.mainThreadStallMs.set(150)
    tuning.thresholds.ownerBlockMs.set(100)
    tuning.thresholds.slowHttpMs.set(500)
    tuning.threads.intervalMs.set(1_000L)
    tuning.concurrency.detailedEnabled.set(
        providers.gradleProperty("jankHunterConcurrencyDetailed").map(String::toBoolean).orElse(false),
    )
    tuning.heapDumps.minIntervalMs.set(1_000)
    tuning.heapDumps.minRetainedAgeMs.set(1_000)
}

dependencies {
    implementation(project(":jankhunter-jankstats"))
    implementation(project(":jankhunter-runtime"))
    implementation(project(":jankhunter-okhttp3"))
    implementation(project(":jankhunter-workmanager"))
    implementation(libs.androidx.work.runtime)
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
    androidTestImplementation(libs.errorprone.annotations)
    testImplementation(libs.junit)
}
