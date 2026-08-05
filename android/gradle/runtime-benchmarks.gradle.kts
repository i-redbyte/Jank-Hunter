import org.gradle.api.tasks.testing.Test

tasks.withType<Test>().configureEach {
    val benchmarkEnabled = providers.systemProperty("jankhunter.benchmark").orElse("false").get()
    systemProperty("jankhunter.benchmark", benchmarkEnabled)
    systemProperty(
        "jankhunter.benchmark.iterations",
        providers.systemProperty("jankhunter.benchmark.iterations").orElse("100000").get(),
    )
    testLogging.showStandardStreams = benchmarkEnabled.toBoolean()
}
