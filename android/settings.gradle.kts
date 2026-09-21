apply(from = "gradle/jankhunter-repository-settings.gradle")

pluginManagement {
    includeBuild("build-logic")
    includeBuild("jankhunter-gradle-plugin")
}

rootProject.name = "JankHunterAndroid"
include(":jankhunter-runtime")
include(":jankhunter-artti")
include(":jankhunter-annotations")
include(":jankhunter-okhttp3")
include(":jankhunter-workmanager")
include(":jankhunter-android-sdk")
include(":jankhunter-cli")
include(":sample-app")
