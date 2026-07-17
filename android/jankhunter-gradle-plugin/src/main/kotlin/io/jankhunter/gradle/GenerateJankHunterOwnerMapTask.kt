package io.jankhunter.gradle

import groovy.json.JsonSlurper
import org.gradle.api.DefaultTask
import org.gradle.api.GradleException
import org.gradle.api.file.ConfigurableFileCollection
import org.gradle.api.file.DirectoryProperty
import org.gradle.api.file.RegularFileProperty
import org.gradle.api.provider.Property
import org.gradle.api.provider.SetProperty
import org.gradle.api.tasks.Input
import org.gradle.api.tasks.InputFiles
import org.gradle.api.tasks.Internal
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
    abstract val lifecycleLeaks: Property<Boolean>

    @get:Input
    abstract val logSpam: Property<Boolean>

    @get:Input
    abstract val classGraph: Property<Boolean>

    @get:Input
    abstract val runtimeCallGraph: Property<Boolean>

    @get:Input
    abstract val generatedOwners: Property<Boolean>

    @get:Input
    abstract val symbolNamespace: Property<String>

    @get:Input
    abstract val includeWholeApplication: Property<Boolean>

    @get:Input
    abstract val androidNamespace: Property<String>

    @get:Input
    abstract val includePackages: SetProperty<String>

    @get:Input
    abstract val excludePackages: SetProperty<String>

    @get:Internal
    abstract val entriesDirectory: DirectoryProperty

    @get:InputFiles
    @get:Optional
    @get:PathSensitive(PathSensitivity.RELATIVE)
    val entryFiles: ConfigurableFileCollection = project.objects.fileCollection()

    @get:OutputFile
    abstract val outputFile: RegularFileProperty

    @TaskAction
    fun write() {
        val file = outputFile.get().asFile
        file.parentFile.mkdirs()
        val entries = entriesDirectory.orNull?.asFile
            ?.let(InstrumentationArtifactFiles::readJsonlLines)
            .orEmpty()
        OwnerMapWriter.validateNoCollisions(entries)
        val lines = buildList {
            add(metadataLine())
            addAll(entries)
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
            lifecycleLeaks = lifecycleLeaks.getOrElse(false),
            logSpam = logSpam.getOrElse(false),
            classGraph = classGraph.getOrElse(false),
            runtimeCallGraph = runtimeCallGraph.getOrElse(false),
            generatedOwners = generatedOwners.getOrElse(false),
            symbolNamespace = symbolNamespace.get(),
            includeWholeApplication = includeWholeApplication.getOrElse(false),
            androidNamespace = androidNamespace.getOrElse(""),
            includePackages = includePackages.getOrElse(emptySet()),
            excludePackages = excludePackages.getOrElse(emptySet()),
        )
    }
}

abstract class MergeJankHunterInstrumentationArtifactsTask : DefaultTask() {
    @get:Internal
    abstract val classGraphDirectory: DirectoryProperty

    @get:InputFiles
    @get:Optional
    @get:PathSensitive(PathSensitivity.RELATIVE)
    val classGraphFiles: ConfigurableFileCollection = project.objects.fileCollection()

    @get:Internal
    abstract val diagnosticsDirectory: DirectoryProperty

    @get:InputFiles
    @get:Optional
    @get:PathSensitive(PathSensitivity.RELATIVE)
    val diagnosticsFiles: ConfigurableFileCollection = project.objects.fileCollection()

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
    val id: Long,
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
        lifecycleLeaks: Boolean,
        logSpam: Boolean,
        classGraph: Boolean,
        runtimeCallGraph: Boolean,
        generatedOwners: Boolean,
        symbolNamespace: String,
        includeWholeApplication: Boolean,
        androidNamespace: String,
        includePackages: Set<String>,
        excludePackages: Set<String>,
    ): String {
        return buildString {
            append("{\"format\":")
            append(ArtifactSchemas.OWNER_MAP_FORMAT)
            append(",\"kind\":\"metadata\"")
            append(",\"variant\":\"")
            append(escape(variantName))
            append("\",\"idAlgorithm\":\"")
            append(escape(OwnerIds.STABLE_ID_ALGORITHM))
            append("\",\"idEncoding\":\"")
            append(escape(OwnerIds.STABLE_ID_ENCODING))
            append('"')
            append(",\"generatedOwners\":")
            append(generatedOwners)
            append(",\"symbolNamespace\":\"")
            append(escape(symbolNamespace))
            append('"')
            append(",\"includeWholeApplication\":")
            append(includeWholeApplication)
            append(",\"hooks\":{")
            appendHook("methodCounters", methodCounters, first = true)
            appendHook("okhttp", okhttp)
            appendHook("webSockets", webSockets)
            appendHook("handlers", handlers)
            appendHook("executors", executors)
            appendHook("coroutines", coroutines)
            appendHook("flowInteractions", flowInteractions)
            appendHook("lifecycleLeaks", lifecycleLeaks)
            appendHook("logSpam", logSpam)
            appendHook("classGraph", classGraph)
            appendHook("runtimeCallGraph", runtimeCallGraph)
            append('}')
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
            .filter { it.owner.isNotBlank() }
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
            append(OwnerIds.canonical(entry.id))
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

    fun validateNoCollisions(records: Iterable<String>) {
        val symbolsById = mutableMapOf<String, OwnerMapSymbol>()
        val parser = JsonSlurper()
        records.forEachIndexed { index, record ->
            val fields = parseRecord(parser, record, index)
            val kind = fields.requiredString("kind", index)
            if (kind != "entry") {
                throw invalidRecord(index, "unsupported kind '$kind'")
            }
            val format = fields["format"]
            if (format != ArtifactSchemas.OWNER_MAP_FORMAT) {
                throw invalidRecord(
                    index,
                    "field 'format' must be ${ArtifactSchemas.OWNER_MAP_FORMAT}",
                )
            }
            val id = fields.requiredString("id", index)
            if (!STABLE_ID.matches(id)) {
                throw invalidRecord(index, "field 'id' is not a stable method id")
            }
            val symbol = OwnerMapSymbol(
                owner = fields.requiredNonBlankString("owner", index),
                className = fields.requiredNonBlankString("class", index),
                methodName = fields.requiredNonBlankString("method", index),
                descriptor = fields.requiredNonBlankString("descriptor", index),
            )
            val previous = symbolsById.putIfAbsent(id, symbol)
            if (previous != null && previous != symbol) {
                throw GradleException(
                    "Jank Hunter stable method id collision for $id: '$previous' and '$symbol'. " +
                        "Instrumentation stopped rather than publishing an ambiguous owner map.",
                )
            }
        }
    }

    private fun parseRecord(parser: JsonSlurper, record: String, index: Int): Map<*, *> {
        val parsed = try {
            parser.parseText(record)
        } catch (cause: RuntimeException) {
            throw invalidRecord(index, "malformed JSON", cause)
        }
        return parsed as? Map<*, *>
            ?: throw invalidRecord(index, "expected a JSON object")
    }

    private fun Map<*, *>.requiredString(field: String, index: Int): String {
        return this[field] as? String
            ?: throw invalidRecord(index, "field '$field' must be a string")
    }

    private fun Map<*, *>.requiredNonBlankString(field: String, index: Int): String {
        return requiredString(field, index).takeIf(String::isNotBlank)
            ?: throw invalidRecord(index, "field '$field' must not be blank")
    }

    private fun invalidRecord(index: Int, detail: String, cause: Throwable? = null): GradleException {
        val message = "Invalid Jank Hunter owner-map record ${index + 1}: $detail."
        return if (cause == null) GradleException(message) else GradleException(message, cause)
    }

    private fun StringBuilder.appendHook(name: String, value: Boolean, first: Boolean = false) {
        if (!first) append(',')
        append('"')
        append(name)
        append("\":")
        append(value)
    }

    private fun array(values: Set<String>): String {
        return values.sorted().joinToString(prefix = "[", postfix = "]") { "\"${escape(it)}\"" }
    }

    private fun escape(value: String): String {
        return value
            .replace("\\", "\\\\")
            .replace("\"", "\\\"")
            .replace("\n", "\\n")
            .replace("\r", "\\r")
    }

    private data class OwnerMapSymbol(
        val owner: String,
        val className: String,
        val methodName: String,
        val descriptor: String,
    )

    private val STABLE_ID = Regex("stable:0x[0-9a-f]{16}")
}
