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
    abstract val okhttp: Property<Boolean>

    @get:Input
    abstract val webSockets: Property<Boolean>

    @get:Input
    abstract val handlers: Property<Boolean>

    @get:Input
    abstract val executors: Property<Boolean>

    @get:Input
    abstract val coroutines: Property<Boolean>

    @get:Input
    abstract val allowEmptyIncludePackages: Property<Boolean>

    @get:Input
    abstract val includeWholeApplication: Property<Boolean>

    @get:Input
    abstract val androidNamespace: Property<String>

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
                appendLine("  \"idAlgorithm\": \"fnv1a64(class.method+descriptor)\",")
                appendLine("  \"hooks\": {")
                appendLine("    \"methodCounters\": ${methodCounters.getOrElse(false)},")
                appendLine("    \"okhttp\": ${okhttp.getOrElse(false)},")
                appendLine("    \"webSockets\": ${webSockets.getOrElse(false)},")
                appendLine("    \"handlers\": ${handlers.getOrElse(false)},")
                appendLine("    \"executors\": ${executors.getOrElse(false)},")
                appendLine("    \"coroutines\": ${coroutines.getOrElse(false)}")
                appendLine("  },")
                appendLine("  \"allowEmptyIncludePackages\": ${allowEmptyIncludePackages.getOrElse(false)},")
                appendLine("  \"includeWholeApplication\": ${includeWholeApplication.getOrElse(false)},")
                appendLine("  \"androidNamespace\": \"${escape(androidNamespace.getOrElse(""))}\",")
                appendLine("  \"includePackages\": ${array(includePackages.getOrElse(emptyList()))},")
                appendLine("  \"excludePackages\": ${array(excludePackages.getOrElse(emptyList()))},")
                appendLine("  \"owners\": {}")
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
