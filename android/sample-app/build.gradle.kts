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
    }
}

dependencies {
    implementation(project(":jankhunter-runtime"))
}
