plugins {
    id("com.android.library")
}

android {
    namespace = "io.jankhunter.runtime"
    compileSdk = 35

    defaultConfig {
        minSdk = 23
        consumerProguardFiles("consumer-rules.pro")
    }
}
