import org.gradle.api.tasks.SourceSetContainer

val pluginMetadataGroup = project.group.toString()
val pluginMetadataVersion = project.version.toString()

val generatePluginMetadata by tasks.registering {
    val outputFile = layout.buildDirectory.file(
        "generated/resources/jankhunterPluginMetadata/io/jankhunter/gradle/jankhunter-plugin.properties",
    )
    inputs.property("jankHunterGroup", pluginMetadataGroup)
    inputs.property("jankHunterVersion", pluginMetadataVersion)
    outputs.file(outputFile)

    doLast {
        val file = outputFile.get().asFile
        file.parentFile.mkdirs()
        file.writeText(
            """
            jankHunterGroup=$pluginMetadataGroup
            jankHunterVersion=$pluginMetadataVersion
            """.trimIndent() + "\n",
        )
    }
}

extensions.configure<SourceSetContainer>("sourceSets") {
    named("main") {
        resources.srcDir(layout.buildDirectory.dir("generated/resources/jankhunterPluginMetadata"))
    }
}

tasks.named("processResources") {
    dependsOn(generatePluginMetadata)
}
