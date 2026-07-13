import org.jetbrains.kotlin.gradle.dsl.JvmTarget
import org.jetbrains.kotlin.gradle.tasks.KotlinCompile

plugins {
    `java-gradle-plugin`
    id("org.jetbrains.kotlin.jvm")
    id("maven-publish")
}

java {
    sourceCompatibility = JavaVersion.VERSION_17
    targetCompatibility = JavaVersion.VERSION_17
}

tasks.withType<JavaCompile>().configureEach {
    options.release.set(17)
}

tasks.withType<KotlinCompile>().configureEach {
    compilerOptions {
        jvmTarget.set(JvmTarget.JVM_17)
    }
}

gradlePlugin {
    plugins {
        create("jankHunterAndroid") {
            id = "io.jankhunter.android"
            implementationClass = "io.jankhunter.gradle.JankHunterPlugin"
        }
    }
}

val jankHunterPluginMetadataGroup = project.group.toString()
val jankHunterPluginMetadataVersion = project.version.toString()

val generateJankHunterPluginMetadata by tasks.registering {
    val outputFile = layout.buildDirectory.file(
        "generated/resources/jankhunterPluginMetadata/io/jankhunter/gradle/jankhunter-plugin.properties",
    )
    inputs.property("jankHunterGroup", jankHunterPluginMetadataGroup)
    inputs.property("jankHunterVersion", jankHunterPluginMetadataVersion)
    outputs.file(outputFile)

    doLast {
        val file = outputFile.get().asFile
        file.parentFile.mkdirs()
        file.writeText(
            """
            jankHunterGroup=$jankHunterPluginMetadataGroup
            jankHunterVersion=$jankHunterPluginMetadataVersion
            """.trimIndent() + "\n",
        )
    }
}

sourceSets {
    main {
        resources.srcDir(layout.buildDirectory.dir("generated/resources/jankhunterPluginMetadata"))
    }
}

tasks.named("processResources") {
    dependsOn(generateJankHunterPluginMetadata)
}

dependencies {
    compileOnly("com.android.tools.build:gradle:9.0.1")
    implementation("org.ow2.asm:asm-commons:9.7.1")
    testImplementation("org.ow2.asm:asm-util:9.7.1")
    testImplementation("junit:junit:4.13.2")
}
