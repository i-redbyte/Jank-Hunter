package io.jankhunter.gradle

import org.gradle.api.DefaultTask
import org.gradle.api.file.DirectoryProperty
import org.gradle.api.file.RegularFileProperty
import org.gradle.api.provider.ListProperty
import org.gradle.api.provider.Property
import org.gradle.api.tasks.Input
import org.gradle.api.tasks.InputDirectory
import org.gradle.api.tasks.Optional
import org.gradle.api.tasks.OutputFile
import org.gradle.api.tasks.PathSensitive
import org.gradle.api.tasks.PathSensitivity
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
    abstract val flowInteractions: Property<Boolean>

    @get:Input
    abstract val logSpam: Property<Boolean>

    @get:Input
    abstract val classGraph: Property<Boolean>

    @get:Input
    abstract val runtimeCallGraph: Property<Boolean>

    @get:Input
    abstract val generatedOwners: Property<Boolean>

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

    @get:InputDirectory
    @get:Optional
    @get:PathSensitive(PathSensitivity.RELATIVE)
    abstract val entriesDirectory: DirectoryProperty

    @get:OutputFile
    abstract val outputFile: RegularFileProperty

    @TaskAction
    fun write() {
        val file = outputFile.get().asFile
        file.parentFile.mkdirs()
        val lines = buildList {
            add(metadataLine())
            entriesDirectory.orNull?.asFile?.let { entriesDir ->
                addAll(InstrumentationArtifactFiles.readJsonlLines(entriesDir))
            }
        }
        file.writeText(lines.joinToString(separator = "\n", postfix = "\n"))
    }

    private fun metadataLine(): String {
        return OwnerMapWriter.metadataRecord(
            variantName = variantName.get(),
            methodCounters = methodCounters.getOrElse(false),
            okhttp = okhttp.getOrElse(false),
            webSockets = webSockets.getOrElse(false),
            handlers = handlers.getOrElse(false),
            executors = executors.getOrElse(false),
            coroutines = coroutines.getOrElse(false),
            flowInteractions = flowInteractions.getOrElse(false),
            logSpam = logSpam.getOrElse(false),
            classGraph = classGraph.getOrElse(false),
            runtimeCallGraph = runtimeCallGraph.getOrElse(false),
            generatedOwners = generatedOwners.getOrElse(false),
            allowEmptyIncludePackages = allowEmptyIncludePackages.getOrElse(false),
            includeWholeApplication = includeWholeApplication.getOrElse(false),
            androidNamespace = androidNamespace.getOrElse(""),
            includePackages = includePackages.getOrElse(emptyList()),
            excludePackages = excludePackages.getOrElse(emptyList()),
        )
    }
}

abstract class MergeJankHunterInstrumentationArtifactsTask : DefaultTask() {
    @get:InputDirectory
    @get:Optional
    @get:PathSensitive(PathSensitivity.RELATIVE)
    abstract val classGraphDirectory: DirectoryProperty

    @get:InputDirectory
    @get:Optional
    @get:PathSensitive(PathSensitivity.RELATIVE)
    abstract val diagnosticsDirectory: DirectoryProperty

    @get:OutputFile
    abstract val classGraphOutputFile: RegularFileProperty

    @get:OutputFile
    abstract val diagnosticsOutputFile: RegularFileProperty

    @TaskAction
    fun merge() {
        InstrumentationArtifactFiles.mergeJsonl(classGraphDirectory.orNull?.asFile, classGraphOutputFile.get().asFile)
        InstrumentationArtifactFiles.mergeJsonl(diagnosticsDirectory.orNull?.asFile, diagnosticsOutputFile.get().asFile)
    }
}

internal data class OwnerMapEntry(
    val id: String,
    val owner: String,
    val className: String,
    val methodName: String,
    val descriptor: String,
)

internal object OwnerMapWriter {
    fun metadataRecord(
        variantName: String,
        methodCounters: Boolean,
        okhttp: Boolean,
        webSockets: Boolean,
        handlers: Boolean,
        executors: Boolean,
        coroutines: Boolean,
        flowInteractions: Boolean,
        logSpam: Boolean,
        classGraph: Boolean,
        runtimeCallGraph: Boolean,
        generatedOwners: Boolean,
        allowEmptyIncludePackages: Boolean,
        includeWholeApplication: Boolean,
        androidNamespace: String,
        includePackages: List<String>,
        excludePackages: List<String>,
    ): String {
        return buildString {
            append("{\"format\":")
            append(ArtifactSchemas.OWNER_MAP_FORMAT)
            append(",\"kind\":\"metadata\"")
            append(",\"variant\":\"")
            append(escape(variantName))
            append("\",\"idAlgorithm\":\"fnv1a64(class.method+descriptor)\"")
            append(",\"generatedOwners\":")
            append(generatedOwners)
            append(",\"hooks\":{")
            appendHook("methodCounters", methodCounters, first = true)
            appendHook("okhttp", okhttp)
            appendHook("webSockets", webSockets)
            appendHook("handlers", handlers)
            appendHook("executors", executors)
            appendHook("coroutines", coroutines)
            appendHook("flowInteractions", flowInteractions)
            appendHook("logSpam", logSpam)
            appendHook("classGraph", classGraph)
            appendHook("runtimeCallGraph", runtimeCallGraph)
            append('}')
            append(",\"allowEmptyIncludePackages\":")
            append(allowEmptyIncludePackages)
            append(",\"includeWholeApplication\":")
            append(includeWholeApplication)
            append(",\"androidNamespace\":\"")
            append(escape(androidNamespace))
            append("\",\"includePackages\":")
            append(array(includePackages))
            append(",\"excludePackages\":")
            append(array(excludePackages))
            append('}')
        }
    }

    fun writeEntries(directoryPath: String, className: String, entries: List<OwnerMapEntry>) {
        if (directoryPath.isBlank() || entries.isEmpty()) return
        val body = entries
            .asSequence()
            .filter { it.id.isNotBlank() && it.owner.isNotBlank() }
            .sortedWith(compareBy<OwnerMapEntry> { it.className }.thenBy { it.methodName }.thenBy { it.descriptor })
            .joinToString(separator = "\n", postfix = "\n") { entryRecord(it) }
        InstrumentationArtifactFiles.writeClassShard(directoryPath, className, body)
    }

    fun entryRecord(entry: OwnerMapEntry): String {
        return buildString {
            append("{\"format\":")
            append(ArtifactSchemas.OWNER_MAP_FORMAT)
            append(",\"kind\":\"entry\"")
            append(",\"id\":\"")
            append(escape(entry.id))
            append("\",\"owner\":\"")
            append(escape(entry.owner))
            append("\",\"class\":\"")
            append(escape(entry.className))
            append("\",\"method\":\"")
            append(escape(entry.methodName))
            append("\",\"descriptor\":\"")
            append(escape(entry.descriptor))
            append("\"}")
        }
    }

    private fun StringBuilder.appendHook(name: String, value: Boolean, first: Boolean = false) {
        if (!first) append(',')
        append('"')
        append(name)
        append("\":")
        append(value)
    }

    private fun array(values: List<String>): String {
        return values.joinToString(prefix = "[", postfix = "]") { "\"${escape(it)}\"" }
    }

    private fun escape(value: String): String {
        return value
            .replace("\\", "\\\\")
            .replace("\"", "\\\"")
            .replace("\n", "\\n")
            .replace("\r", "\\r")
    }
}
