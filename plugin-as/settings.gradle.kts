pluginManagement {
    repositories {
        maven { url = uri("https://registry.vktech.team/repository/maven-gradle-plugins-remote-internal-proxy/") }
        maven { url = uri("https://registry.vktech.team/repository/maven-internal-proxy/") }
        maven { url = uri("https://nexus.vkteam.ru/repository/maven-gradle-plugins-remote/") }
        maven { url = uri("https://nexus.vkteam.ru/repository/maven/") }
        gradlePluginPortal()
        mavenCentral()
    }
}

dependencyResolutionManagement {
    repositories {
        maven { url = uri("https://registry.vktech.team/repository/maven-internal-proxy/") }
        maven { url = uri("https://nexus.vkteam.ru/repository/maven/") }
        mavenCentral()
    }
}

rootProject.name = "jank-hunter-android-studio-plugin"
