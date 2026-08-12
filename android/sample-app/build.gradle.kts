plugins {
    id("io.jankhunter.android-application")
    id("io.jankhunter.android")
}

val sampleJankHunterEnabled = providers.gradleProperty("jankhunter.sample.enabled")
    .map { it.toBooleanStrict() }
    .orElse(true)
val sampleArtTiMode = providers.gradleProperty("jankhunter.sample.artTiMode")
    .map { io.jankhunter.gradle.ArtTiMode.valueOf(it.uppercase()) }
    .orElse(io.jankhunter.gradle.ArtTiMode.CAUSAL)

android {
    namespace = "io.jankhunter.sample"

    defaultConfig {
        applicationId = "io.jankhunter.sample"
        versionCode = 1
    }
    buildTypes {
        getByName("release") {
            isMinifyEnabled = true
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro",
            )
        }
    }
}

jankHunter {
    // Reproducible performance lanes:
    //   -Pjankhunter.sample.enabled=false (SDK baseline)
    //   -Pjankhunter.sample.artTiMode=OFF (SDK with agent disabled)
    //   default / CAUSAL (recommended agent profile)
    enabled.set(sampleJankHunterEnabled)
    enabledBuildTypes.set(setOf("debug"))
    autoInit.set(true)
    artTi {
        mode.set(sampleArtTiMode)
    }
    verboseLogs.set(true)

    runtime {
        mainThreadStallThresholdMs.set(150)
        ownerBlockThresholdMs.set(100)
        httpSlowThresholdMs.set(500)
        jankFrameThresholdMs.set(32)
        uiWindowP95ThresholdMs.set(32)
        mainLooperDispatchMonitor.set(true)
        jankStats.set(true)
        mainProcessOnly.set(true)
    }

    instrument {
        classGraph.set(true)
        runtimeCallGraph.set(true)
        methodCounters.set(false)
        okhttp.set(true)
        webSockets.set(true)
        handlers.set(true)
        executors.set(true)
        coroutines.set(true)
        flowInteractions.set(true)
        lifecycleLeaks.set(true)
        logSpam.set(true)
        includeAndroidNamespace.set(false)
        includePackages("io.jankhunter.sample.graph")
    }

    retainedHeapDump {
        enabled.set(true)
        privacyApproved.set(true)
        minIntervalMs.set(1_000)
        maxCount.set(1)
        minRetainedAgeMs.set(1_000)
    }
}

dependencies {
    implementation(project(":jankhunter-runtime"))
    implementation(project(":jankhunter-okhttp3"))
    implementation(libs.okhttp)
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
