plugins {
    id("io.jankhunter.android-library")
}

android {
    namespace = "io.jankhunter.sdk"
}

dependencies {
    api(project(":jankhunter-annotations"))
    api(project(":jankhunter-runtime"))
    api(project(":jankhunter-okhttp3"))
}
