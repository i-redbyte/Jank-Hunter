plugins {
    `kotlin-dsl`
}

tasks.withType<org.jetbrains.kotlin.gradle.tasks.KotlinCompile>().configureEach {
    compilerOptions.allWarningsAsErrors.set(true)
}

dependencies {
    implementation(libs.android.gradle.plugin)
    implementation(libs.detekt.gradle.plugin)
    implementation(libs.kotlin.compose.gradle.plugin)
    implementation(libs.kotlin.gradle.plugin)
    testImplementation(libs.junit)
}

gradlePlugin {
    plugins {
        register("rootConventions") {
            id = "io.jankhunter.root-conventions"
            implementationClass = "io.jankhunter.buildlogic.JankHunterRootConventionsPlugin"
        }
        register("androidLibrary") {
            id = "io.jankhunter.android-library"
            implementationClass = "io.jankhunter.buildlogic.JankHunterAndroidLibraryPlugin"
        }
        register("androidApplication") {
            id = "io.jankhunter.android-application"
            implementationClass = "io.jankhunter.buildlogic.JankHunterAndroidApplicationPlugin"
        }
        register("kotlinLibrary") {
            id = "io.jankhunter.kotlin-library"
            implementationClass = "io.jankhunter.buildlogic.JankHunterKotlinLibraryPlugin"
        }
        register("kotlinGradlePlugin") {
            id = "io.jankhunter.kotlin-gradle-plugin"
            implementationClass = "io.jankhunter.buildlogic.JankHunterKotlinGradlePlugin"
        }
        register("sqlNormalizerSources") {
            id = "io.jankhunter.sql-normalizer-sources"
            implementationClass = "io.jankhunter.buildlogic.JankHunterSqlNormalizerSourcesPlugin"
        }
    }
}
