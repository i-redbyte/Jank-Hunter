import java.util.Properties

plugins {
    `java-gradle-plugin`
    id("io.jankhunter.kotlin-gradle-plugin")
}

val sharedProperties = Properties().apply {
    file("../gradle.properties").inputStream().use(::load)
}

group = providers.gradleProperty("jankHunterGroup")
    .getOrElse(sharedProperties.getProperty("jankHunterGroup"))
version = providers.gradleProperty("jankHunterVersion")
    .getOrElse(sharedProperties.getProperty("jankHunterVersion"))

gradlePlugin {
    plugins {
        create("jankHunterAndroid") {
            id = "io.jankhunter.android"
            implementationClass = "io.jankhunter.gradle.JankHunterPlugin"
        }
    }
}

dependencies {
    compileOnly(libs.android.gradle.plugin)
    implementation(libs.asm.commons)
    testImplementation(libs.android.gradle.plugin)
    testImplementation(libs.asm.util)
    testImplementation(libs.junit)
}

apply(from = file("../gradle/plugin-metadata.gradle.kts"))
