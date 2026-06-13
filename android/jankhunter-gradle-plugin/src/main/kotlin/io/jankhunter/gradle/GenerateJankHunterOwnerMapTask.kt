package io.jankhunter.gradle

import org.gradle.api.DefaultTask
import org.gradle.api.file.RegularFileProperty
import org.gradle.api.provider.ListProperty
import org.gradle.api.provider.Property
import org.gradle.api.tasks.Input
import org.gradle.api.tasks.OutputFile
import org.gradle.api.tasks.TaskAction

abstract class GenerateJankHunterOwnerMapTask : DefaultTask() {
    @get:Input
    abstract val variantName: Property<String>

    @get:Input
    abstract val methodCounters: Property<Boolean>

    @get:Input
    abstract val includePackages: ListProperty<String>

    @get:Input
    abstract val excludePackages: ListProperty<String>

    @get:OutputFile
    abstract val outputFile: RegularFileProperty

    @TaskAction
    fun write() {
        val file = outputFile.get().asFile
        file.parentFile.mkdirs()
        file.writeText(
            buildString {
                appendLine("{")
                appendLine("  \"format\": 1,")
                appendLine("  \"variant\": \"${escape(variantName.get())}\",")
                appendLine("  \"methodCounters\": ${methodCounters.getOrElse(false)},")
                appendLine("  \"includePackages\": ${array(includePackages.getOrElse(emptyList()))},")
                appendLine("  \"excludePackages\": ${array(excludePackages.getOrElse(emptyList()))}")
                appendLine("}")
            },
        )
    }

    private fun array(values: List<String>): String {
        return values.joinToString(prefix = "[", postfix = "]") { "\"${escape(it)}\"" }
    }

    private fun escape(value: String): String {
        return value.replace("\\", "\\\\").replace("\"", "\\\"")
    }
}
