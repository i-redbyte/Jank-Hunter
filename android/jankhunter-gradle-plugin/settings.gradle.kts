pluginManagement {
    includeBuild("../build-logic")
    repositories {
        maven {
            url = uri("https://registry.vktech.team/repository/maven-gradle-plugins-remote-internal-proxy/")
        }
        maven {
            url = uri("https://registry.vktech.team/repository/maven-internal-proxy/")
        }
        maven {
            url = uri("https://nexus.vkteam.ru/repository/maven-gradle-plugins-remote/")
        }
        maven { url = uri("https://nexus.vkteam.ru/repository/maven/") }
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}

dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        maven {
            url = uri("https://registry.vktech.team/repository/maven-internal-proxy/")
        }
        maven { url = uri("https://nexus.vkteam.ru/repository/maven/") }
        google()
        mavenCentral()
        gradlePluginPortal()
    }
    versionCatalogs {
        create("libs") {
            from(files("../gradle/libs.versions.toml"))
        }
    }
}

rootProject.name = "jankhunter-gradle-plugin"

include(":metadata-reader")
