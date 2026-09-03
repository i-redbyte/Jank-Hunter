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
}

dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        maven {
            url = uri("https://registry.vktech.team/repository/maven-internal-proxy/")
        }
        mavenCentral()
        gradlePluginPortal()
    }
    versionCatalogs {
        create("libs") {
            from(files("../gradle/libs.versions.toml"))
        }
    }
}

rootProject.name = "jankhunter-build-logic"
