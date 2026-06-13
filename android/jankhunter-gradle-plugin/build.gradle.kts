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

dependencies {
    compileOnly("com.android.tools.build:gradle:9.0.1")
    testImplementation("junit:junit:4.13.2")
}
