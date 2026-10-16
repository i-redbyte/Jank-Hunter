import java.util.Properties
import java.security.MessageDigest

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
    testImplementation("org.jetbrains.kotlin:kotlin-metadata-jvm:2.0.21")
    implementation(libs.h2.mvstore)
    testImplementation(libs.android.gradle.plugin)
    testImplementation(libs.androidx.room.runtime)
    testImplementation(libs.asm.util)
    testImplementation(libs.junit)
    testImplementation(libs.okhttp)
    testImplementation(gradleTestKit())
}

// TestKit injects a separate plugin loader: compileOnly AGP must be supplied to that loader.
// This configuration is test infrastructure and is never published with the plugin.
val testKitAgp by configurations.creating {
    isCanBeConsumed = false
    isCanBeResolved = true
}
dependencies { add(testKitAgp.name, libs.android.gradle.plugin) }
tasks.named<org.gradle.plugin.devel.tasks.PluginUnderTestMetadata>("pluginUnderTestMetadata") {
    pluginClasspath.from(testKitAgp)
}

apply(from = file("../gradle/plugin-metadata.gradle.kts"))
apply(from = file("../gradle/runtime-benchmarks.gradle.kts"))

tasks.register<JavaExec>("writeOkHttpTransportFixture") {
    dependsOn(tasks.named("testClasses"))
    classpath = sourceSets.test.get().runtimeClasspath
    mainClass.set("io.jankhunter.gradle.OkHttpTransportFixture")
    val output = layout.buildDirectory.file("test-fixtures/okhttp-transport.jar")
    outputs.file(output)
    inputs.files(classpath)
    args(output.get().asFile.absolutePath)
}

tasks.register<JavaExec>("checkKotlinMetadataFixtures") {
    group = "verification"
    description = "Checks real Kotlin 1.9–2.3 and Java 8/11/17/21 compiler fixtures and preserves their metadata through ASM."
    dependsOn(tasks.named("testClasses"))
    classpath = sourceSets.test.get().runtimeClasspath
    mainClass.set("io.jankhunter.gradle.MetadataFixtureCheck")
    doFirst {
        args(providers.gradleProperty("metadataFixtureDirectory").get())
    }
}

tasks.register<JavaExec>("benchmarkMetadataReader") {
    group = "verification"
    description = "Measures real isolated metadata decoding on the build host."
    dependsOn(tasks.named("testClasses"))
    classpath = sourceSets.test.get().runtimeClasspath
    mainClass.set("io.jankhunter.gradle.MetadataReaderCostProbe")
    args(providers.gradleProperty("metadataBenchmarkIterations").getOrElse("100000"),
        providers.gradleProperty("metadataBenchmarkWorkers").getOrElse("1"))
}


// Reader/compiler dependencies are private jar resources, never plugin DSL or APK dependencies.
val isolatedMetadataReader by configurations.creating {
    isCanBeConsumed = false
    isCanBeResolved = true
}
dependencies { add(isolatedMetadataReader.name, project(":metadata-reader")) }
tasks.named<ProcessResources>("processResources") {
    from(isolatedMetadataReader) { into("META-INF/jankhunter/metadata-reader") }
    doLast {
        val folder = destinationDir.resolve("META-INF/jankhunter/metadata-reader")
        folder.resolve("index").writeText(isolatedMetadataReader.files.map { it.name }.sorted().joinToString("\n"))
        val capabilities = Properties().apply {
            setProperty("schema", "1")
            setProperty("reader.id", "kotlin-jvm-2.3.20")
            setProperty("bridge.protocol", "1")
            setProperty("reader.priority", "10")
            setProperty("metadata.families", "1.1,1.4,1.5,1.6,1.7,1.8,1.9,2.0,2.1,2.2,2.3")
            setProperty("metadata.kinds", "1,2,3,4,5")
            setProperty("metadata.flags", "255")
            isolatedMetadataReader.files.forEach { jar ->
                val digest = MessageDigest.getInstance("SHA-256").digest(jar.readBytes())
                    .joinToString("") { "%02x".format(it) }
                setProperty("jar.${jar.name}", digest)
            }
        }
        folder.resolve("capabilities.properties").writeText(capabilities.stringPropertyNames().sorted()
            .joinToString("\n", postfix = "\n") { "$it=${capabilities.getProperty(it)}" })
    }
}

// Producer-only compile boundary; this does not publish a constraint or alter consumers.
configurations.matching { it.name == "compileClasspath" || it.name == "testCompileClasspath" }.configureEach {
    resolutionStrategy.force("org.jetbrains.kotlin:kotlin-stdlib:1.9.25")
}

// Gradle's own Kotlin jars are a host implementation detail. Compile the portable DSL
// against its declared stdlib, while retaining the Gradle API and project dependencies.
tasks.withType<org.jetbrains.kotlin.gradle.tasks.KotlinCompile>().configureEach {
    val compilation = if (name == "compileTestKotlin") "test" else "main"
    libraries.setFrom(sourceSets.named(compilation).map { sourceSet ->
        sourceSet.compileClasspath.filter { file ->
            !(file.parentFile.name == "lib" && file.name.startsWith("kotlin-"))
        }
    })
}

val historicalFixtureCompilers = listOf("1.3.72", "1.4.32", "1.5.31", "1.6.21", "1.7.22", "1.8.22")
val fixtureCompilerCoordinates = (historicalFixtureCompilers + listOf("1.9.23", "2.0.21", "2.1.21", "2.2.21", "2.3.20")).flatMap { compiler ->
    listOf("org.jetbrains.kotlin:kotlin-compiler-embeddable:$compiler",
        "org.jetbrains.kotlin:kotlin-stdlib:${if (compiler == "1.9.23") "1.9.10" else compiler}") +
        if (compiler in historicalFixtureCompilers) listOf("org.jetbrains.kotlin:kotlin-reflect:$compiler", "org.jetbrains.kotlin:kotlin-script-runtime:$compiler") else emptyList()
} + listOf("org.jetbrains:annotations:13.0", "org.jetbrains.intellij.deps:trove4j:1.0.20200330",
    "org.jetbrains.kotlinx:kotlinx-coroutines-core-jvm:1.9.0", "org.jetbrains.kotlin:kotlin-reflect:1.6.10",
    "org.jetbrains.kotlin:kotlin-reflect:2.2.21")
val fixtureCompilerInputs = fixtureCompilerCoordinates.mapIndexed { index, coordinate ->
    configurations.create("metadataFixtureCompilerInput$index") {
        isCanBeConsumed = false
        isCanBeResolved = true
        isTransitive = false
    }.also { dependencies.add(it.name, coordinate) }
}
tasks.register("resolveMetadataFixtureCompilers") {
    doLast { fixtureCompilerInputs.forEach { it.resolve() } }
}
