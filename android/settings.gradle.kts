pluginManagement {
    includeBuild("build-logic")
    includeBuild("jankhunter-gradle-plugin")
    repositories {
        maven {
            url = uri("https://registry.vktech.team/repository/maven-gradle-plugins-remote-internal-proxy/")
        }
        maven {
            url = uri("https://nexus.vkteam.ru/repository/maven-gradle-plugins-remote/")
        }
        google()
        mavenCentral()
        gradlePluginPortal()
    }
    resolutionStrategy {
        eachPlugin {
            if (requested.id.id == "com.android.library" || requested.id.id == "com.android.application") {
                useModule("com.android.tools.build:gradle:${requested.version}")
            }
        }
    }
}

dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        maven {
            url = uri("https://registry.vktech.team/repository/maven-internal-proxy/")
        }
        mavenCentral()
    }
}

rootProject.name = "JankHunterAndroid"
include(":jankhunter-runtime")
include(":jankhunter-annotations")
include(":jankhunter-okhttp3")
include(":jankhunter-workmanager")
include(":jankhunter-android-sdk")
include(":sample-app")
