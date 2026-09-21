apply(from = "../gradle/jankhunter-repository-settings.gradle")

dependencyResolutionManagement {
    versionCatalogs {
        create("libs") {
            from(files("../gradle/libs.versions.toml"))
        }
    }
}

rootProject.name = "jankhunter-build-logic"
