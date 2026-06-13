plugins {
    id("com.android.application")
}

android {
    namespace = "io.jankhunter.sample"
    compileSdk = 35

    defaultConfig {
        applicationId = "io.jankhunter.sample"
        minSdk = 23
        targetSdk = 35
        versionCode = 1
        versionName = "0.1.0"
    }
}

dependencies {
    implementation(project(":jankhunter-runtime"))
}
