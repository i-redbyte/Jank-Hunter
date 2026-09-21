plugins {
    id("io.jankhunter.root-conventions")
}

val publishToMavenLocal by tasks.registering {
    group = "publishing"
    description = "Publishes all Jank Hunter modules, including the Gradle plugin and marker."
    dependsOn(gradle.includedBuild("jankhunter-gradle-plugin").task(":publishToMavenLocal"))
}

subprojects {
    plugins.withId("maven-publish") {
        val modulePublication = tasks.named("publishToMavenLocal")
        publishToMavenLocal.configure {
            dependsOn(modulePublication)
        }
    }
}
