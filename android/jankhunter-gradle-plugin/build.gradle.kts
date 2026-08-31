import java.util.Properties

plugins {
    `java-gradle-plugin`
    id("io.jankhunter.kotlin-gradle-plugin")
    id("io.jankhunter.sql-normalizer-sources")
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
    implementation(libs.asm.analysis)
    implementation(libs.asm.commons)
    implementation(libs.asm.tree)
    implementation(libs.kotlin.metadata.jvm)
    testImplementation(libs.android.gradle.plugin)
    testImplementation(libs.androidx.room.runtime)
    testImplementation(libs.asm.util)
    testImplementation(libs.junit)
}

apply(from = file("../gradle/plugin-metadata.gradle.kts"))
