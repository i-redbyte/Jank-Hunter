plugins {
    `java-gradle-plugin`
    id("org.jetbrains.kotlin.jvm")
}

gradlePlugin {
    plugins {
        create("jankHunterAndroid") {
            id = "io.jankhunter.android"
            implementationClass = "io.jankhunter.gradle.JankHunterPlugin"
        }
    }
}
