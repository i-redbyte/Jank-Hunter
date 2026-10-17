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

    androidTestImplementation(libs.bundles.androidx.test)
    androidTestImplementation(libs.androidx.work.runtime)
    androidTestImplementation(project(":jankhunter-jankstats"))
    testImplementation(project(":jankhunter-jankstats"))
    testImplementation(libs.androidx.metrics.performance)
    androidTestImplementation(libs.androidx.metrics.performance)
    testImplementation(libs.junit)
    testImplementation(libs.androidx.work.runtime)
}

apply(from = rootProject.file("gradle/runtime-benchmarks.gradle.kts"))

tasks.register<JavaExec>("checkHotPathAdmission") {
    group = "verification"
    description = "Verifies FULL instrumentation of generic synchronous callbacks against a stalled consumer."
    dependsOn("compileDebugUnitTestKotlin", "compileDebugUnitTestJavaWithJavac", "processDebugUnitTestJavaRes")
    classpath = files(provider { tasks.named<org.gradle.api.tasks.testing.Test>("testDebugUnitTest").get().classpath })
    mainClass.set("io.jankhunter.runtime.HotPathAdmissionCheck")
    doFirst {
        args(providers.gradleProperty("hotPathFixtureJar").get(), providers.gradleProperty("hotPathEvidence").get())
    }
}
