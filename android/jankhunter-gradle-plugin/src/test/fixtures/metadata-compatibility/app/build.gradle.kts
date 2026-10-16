plugins {
    id("com.android.application")
    id("io.jankhunter.android")
}
android {
    namespace = "com.fixture.app"
    compileSdk = 35
    defaultConfig { applicationId = "com.fixture.app"; minSdk = 28; targetSdk = 35 }
    compileOptions { sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
}
kotlin { compilerOptions { jvmTarget.set(org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17) } }
jankHunter {
    profile.set(io.jankhunter.gradle.JankHunterProfile.MINIMAL)
    if (providers.gradleProperty("fixtureParallelVariants").getOrElse("false").toBoolean()) {
        enabledBuildTypes.set(setOf("debug", "release"))
        release {
            privacyReviewed()
            performanceBudget(file("fixture-release-budget.txt"))
        }
    }
    autoInit.set(false)
    packages("com.fixture.app")
    disable(*io.jankhunter.gradle.JankHunterFeature.entries.filter { it !in setOf(io.jankhunter.gradle.JankHunterFeature.CALL_GRAPH, io.jankhunter.gradle.JankHunterFeature.CLASS_GRAPH, io.jankhunter.gradle.JankHunterFeature.DI_ANALYSIS) }.toTypedArray())
    enable(io.jankhunter.gradle.JankHunterFeature.CALL_GRAPH, io.jankhunter.gradle.JankHunterFeature.CLASS_GRAPH, io.jankhunter.gradle.JankHunterFeature.DI_ANALYSIS)
    metadataCompatibilityMode.set(providers.gradleProperty("fixtureMetadataMode").map { io.jankhunter.gradle.JankHunterMetadataCompatibilityMode.valueOf(it) }.orElse(io.jankhunter.gradle.JankHunterMetadataCompatibilityMode.COMPATIBILITY))
}
dependencies {
    implementation(project(":feature"))
    implementation(project(":jankhunter-runtime"))
    compileOnly(project(":jankhunter-annotations"))
}

providers.gradleProperty("futureMetadataJar").orNull?.let { jar -> dependencies.add("runtimeOnly", files(jar)) }
