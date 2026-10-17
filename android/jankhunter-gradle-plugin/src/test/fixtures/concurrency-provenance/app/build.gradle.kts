plugins {
    id("com.android.application") version "9.0.1"
    id("io.jankhunter.android")
}

android {
    namespace = "com.fixture.app"
    compileSdk = 35
    defaultConfig {
        applicationId = "com.fixture.app"
        minSdk = 23
        targetSdk = 35
    }
    buildTypes {
        release {
            isMinifyEnabled = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"))
        }
    }
}

jankHunter {
    val threadSamplingOnly = providers.gradleProperty("threadSamplingOnly")
        .map(String::toBoolean).getOrElse(false)
    if (providers.gradleProperty("wholeApplication").map(String::toBoolean).getOrElse(false)) {
        scope.set(io.jankhunter.gradle.JankHunterInstrumentationScope.WHOLE_APPLICATION)
    }
    enabled.set(true)
    enabledBuildTypes.set(setOf("debug", "release"))
    profile.set(io.jankhunter.gradle.JankHunterProfile.TARGETED)
    packages("com.fixture.app")
    autoInit.set(false)
    debug {
        enable(io.jankhunter.gradle.JankHunterFeature.THREAD_SAMPLING)
        if (!threadSamplingOnly) enable(io.jankhunter.gradle.JankHunterFeature.CONCURRENCY_DIAGNOSTICS)
    }
    release {
        enable(io.jankhunter.gradle.JankHunterFeature.THREAD_SAMPLING)
        if (!threadSamplingOnly) enable(io.jankhunter.gradle.JankHunterFeature.CONCURRENCY_DIAGNOSTICS)
        privacyReviewed()
        performanceBudget(project.file("release-budget.txt"))
    }
}

dependencies {
    implementation(project(":feature"))
    runtimeOnly(project(":runtime-only"))
    implementation("com.fixture:included:1.0")
    implementation("com.fixture:external:1.0")
    implementation(files("libs/unknown.jar"))
    implementation(project(":jankhunter-runtime"))
    compileOnly(project(":jankhunter-annotations"))
}
