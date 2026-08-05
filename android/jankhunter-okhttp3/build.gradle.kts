plugins {
    id("io.jankhunter.android-library")
}

android {
    namespace = "io.jankhunter.okhttp3"

    defaultConfig {
        consumerProguardFiles("consumer-rules.pro")
    }
}

dependencies {
    implementation(project(":jankhunter-runtime"))
    compileOnly(libs.okhttp)
    testImplementation(libs.okhttp)
    testImplementation(libs.junit)
}
