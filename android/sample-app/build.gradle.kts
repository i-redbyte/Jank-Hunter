plugins {
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
    autoInit.set(true)
    verboseLogs.set(true)
    sessionLogSizeLimitEnabled.set(true)
    maxSessionLogSizeMiB.set(50)
    logGrowthAnalyticsEnabled.set(true)

    runtime {
        mainThreadStallThresholdMs.set(150)
        ownerBlockThresholdMs.set(100)
        httpSlowThresholdMs.set(500)
        jankFrameThresholdMs.set(32)
        uiWindowP95ThresholdMs.set(32)
        exactEventCollection.set(true)
        maxQueueSize.set(65_536)
        mainLooperDispatchMonitor.set(true)
        jankStats.set(true)
        mainProcessOnly.set(false)
    }

    instrument {
        classGraph.set(true)
        runtimeCallGraph.set(true)
        methodCounters.set(true)
        okhttp.set(true)
        webSockets.set(true)
        handlers.set(true)
        executors.set(true)
        coroutines.set(true)
        flowInteractions.set(true)
        lifecycleLeaks.set(true)
        logSpam.set(true)
        includeAndroidNamespace.set(true)
        includeWholeApplication.set(false)
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
