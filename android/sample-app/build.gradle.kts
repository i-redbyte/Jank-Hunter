plugins {
    id("com.android.application")
}

val jankHunterVersion = providers.gradleProperty("jankHunterVersion").get()

android {
    namespace = "io.jankhunter.sample"
    compileSdk = 35

    defaultConfig {
        applicationId = "io.jankhunter.sample"
        minSdk = 23
        targetSdk = 35
        versionCode = 1
        versionName = jankHunterVersion.removeSuffix("-SNAPSHOT")
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }
}

dependencies {
    implementation(project(":jankhunter-runtime"))
    androidTestImplementation("androidx.test:core:1.6.1")
    androidTestImplementation("androidx.test:runner:1.6.2")
    androidTestImplementation("androidx.test.ext:junit:1.2.1")
}
