plugins {
    id("com.android.library")
}

android {
    namespace = "io.jankhunter.okhttp3"
    compileSdk = 35

    defaultConfig {
        minSdk = 23
        consumerProguardFiles("consumer-rules.pro")
    }
}

dependencies {
    api(project(":jankhunter-runtime"))
    compileOnly("com.squareup.okhttp3:okhttp:3.12.13")
}
