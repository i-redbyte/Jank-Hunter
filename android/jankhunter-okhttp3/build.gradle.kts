plugins {
    id("io.jankhunter.android-library")
}

android {
    namespace = "io.jankhunter.okhttp3"

    defaultConfig {
        consumerProguardFiles("consumer-rules.pro")
    }

    sourceSets.getByName("test").kotlin.directories.add("src/sharedTest/kotlin")
    sourceSets.getByName("androidTest").kotlin.directories.add("src/sharedTest/kotlin")
    sourceSets.getByName("test").resources.srcDir("src/sharedTest/resources")
    sourceSets.getByName("androidTest").resources.srcDir("src/sharedTest/resources")
}

val transportFixtureLibraries = configurations.create("transportFixtureLibraries") {
    isCanBeConsumed = false
}
val transportFixture = files(rootProject.file("jankhunter-gradle-plugin/build/test-fixtures/okhttp-transport.jar"))
    .builtBy(gradle.includedBuild("jankhunter-gradle-plugin").task(":writeOkHttpTransportFixture"))
val transportFixtureDependencies = transportFixtureLibraries.filter { !it.name.startsWith("okhttp-") }

dependencies {
    implementation(project(":jankhunter-runtime"))
    compileOnly(libs.okhttp)
    add(transportFixtureLibraries.name, libs.okhttp)
    testImplementation(transportFixture)
    testImplementation(transportFixtureDependencies)
    testImplementation(libs.junit)
    androidTestImplementation(transportFixture)
    androidTestImplementation(transportFixtureDependencies)
    androidTestImplementation(libs.bundles.androidx.test)
}

apply(from = rootProject.file("gradle/runtime-benchmarks.gradle.kts"))
