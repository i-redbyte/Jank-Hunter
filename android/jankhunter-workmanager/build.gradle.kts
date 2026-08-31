plugins {
    id("io.jankhunter.android-library")
}

android {
    namespace = "io.jankhunter.workmanager"

    testOptions {
        unitTests.isReturnDefaultValues = true
    }
}

dependencies {
    compileOnly(project(":jankhunter-runtime"))
    compileOnly(libs.androidx.work.runtime)

    testImplementation(project(":jankhunter-runtime"))
    testImplementation(libs.androidx.work.runtime)
    testImplementation(libs.junit)
}
