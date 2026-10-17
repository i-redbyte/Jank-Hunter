pluginManagement {
    includeBuild("../../../../")
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
    resolutionStrategy {
        eachPlugin {
            if (requested.id.id == "com.android.application") {
                useModule("com.android.tools.build:gradle:${requested.version}")
            }
        }
    }
}

dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        maven(url = uri("fixture-maven"))
        google()
        mavenCentral()
    }
}

rootProject.name = "concurrency-provenance-fixture"
include(":app", ":feature", ":runtime-only", ":jankhunter-runtime", ":jankhunter-annotations")
includeBuild("included")
