pluginManagement {
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
            if (requested.id.id == "org.jetbrains.kotlin.android") {
                useModule("org.jetbrains.kotlin:kotlin-gradle-plugin:${requested.version}")
            }
            if (requested.id.id == "com.android.application") {
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

rootProject.name = "metadata-compatibility-fixture"
include(":app", ":feature", ":jankhunter-runtime", ":jankhunter-annotations")
