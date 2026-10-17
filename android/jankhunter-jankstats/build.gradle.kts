plugins { id("io.jankhunter.android-library") }

android {
    namespace = "io.jankhunter.jankstats"
    defaultConfig { consumerProguardFiles("consumer-rules.pro") }
}
dependencies {
    compileOnly(project(":jankhunter-runtime"))
    implementation(libs.androidx.metrics.performance)
}
