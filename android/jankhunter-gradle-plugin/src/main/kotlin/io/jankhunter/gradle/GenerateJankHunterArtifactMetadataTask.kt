package io.jankhunter.gradle

import org.gradle.api.DefaultTask
import org.gradle.api.file.ConfigurableFileCollection
import org.gradle.api.file.DirectoryProperty
import org.gradle.api.file.RegularFileProperty
import org.gradle.api.provider.Property
import org.gradle.api.provider.SetProperty
import org.gradle.api.tasks.Input
import org.gradle.api.tasks.CacheableTask
import org.gradle.api.tasks.InputFiles
import org.gradle.api.tasks.Internal
import org.gradle.api.tasks.Optional
import org.gradle.api.tasks.OutputFile
import org.gradle.api.tasks.PathSensitive
import org.gradle.api.tasks.PathSensitivity
import org.gradle.api.tasks.TaskAction

@CacheableTask
abstract class GenerateJankHunterArtifactMetadataTask : DefaultTask() {
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
    abstract val interactionOperations: Property<Boolean>

    @get:Input
    abstract val lifecycleLeaks: Property<Boolean>

    @get:Input
    abstract val logSpam: Property<Boolean>

    @get:Input
    abstract val classGraph: Property<Boolean>

    @get:Input
    abstract val runtimeCallGraph: Property<Boolean>

    @get:Input
    abstract val symbolNamespace: Property<String>

    @get:Input
    abstract val includeWholeApplication: Property<Boolean>

    @get:Input
    abstract val networkWholeApplication: Property<Boolean>

    @get:Input
    abstract val databaseWholeApplication: Property<Boolean>

    @get:Input
    abstract val databaseTracing: Property<Boolean>

    @get:Input
    abstract val ioTracing: Property<Boolean>

    @get:Input
    abstract val androidNamespace: Property<String>

    @get:Input
    abstract val includePackages: SetProperty<String>

    @get:Input
    abstract val excludePackages: SetProperty<String>

    @get:OutputFile
    abstract val outputFile: RegularFileProperty

    @TaskAction
    fun write() {
        val file = outputFile.get().asFile
        file.parentFile.mkdirs()
        file.writeText(metadataRecord() + '\n')
    }

    private fun metadataRecord(): String {
        return buildString(512) {
            append("{\"format\":")
            append(ArtifactSchemas.ARTIFACT_METADATA_FORMAT)
            append(",\"kind\":\"artifact-metadata\"")
            append(",\"variant\":\"")
            append(escapeJsonString(variantName.get()))
            append("\",\"idAlgorithm\":\"")
            append(escapeJsonString(OwnerIds.STABLE_ID_ALGORITHM))
            append("\",\"idEncoding\":\"")
            append(escapeJsonString(OwnerIds.STABLE_ID_ENCODING))
            append("\",\"symbolNamespace\":\"")
            append(escapeJsonString(symbolNamespace.get()))
            append("\",\"includeWholeApplication\":")
            append(includeWholeApplication.getOrElse(false))
            append(",\"networkWholeApplication\":")
            append(networkWholeApplication.getOrElse(true))
            append(",\"databaseWholeApplication\":")
            append(databaseWholeApplication.getOrElse(true))
            append(",\"hooks\":{")
            appendHook("methodCounters", methodCounters.getOrElse(false), first = true)
            appendHook("okhttp", okhttp.getOrElse(false))
            appendHook("webSockets", webSockets.getOrElse(false))
            appendHook("handlers", handlers.getOrElse(false))
            appendHook("executors", executors.getOrElse(false))
            appendHook("coroutines", coroutines.getOrElse(false))
            appendHook("interactionOperations", interactionOperations.getOrElse(false))
            appendHook("lifecycleLeaks", lifecycleLeaks.getOrElse(false))
            appendHook("logSpam", logSpam.getOrElse(false))
            appendHook("classGraph", classGraph.getOrElse(false))
            appendHook("runtimeCallGraph", runtimeCallGraph.getOrElse(false))
            appendHook("databaseTracing", databaseTracing.getOrElse(false))
            appendHook("ioTracing", ioTracing.getOrElse(false))
            append('}')
            append(",\"androidNamespace\":\"")
            append(escapeJsonString(androidNamespace.getOrElse("")))
            append("\",\"includePackages\":")
            appendJsonArray(includePackages.getOrElse(emptySet()))
            append(",\"excludePackages\":")
            appendJsonArray(excludePackages.getOrElse(emptySet()))
            append('}')
        }
    }

    private fun StringBuilder.appendHook(name: String, value: Boolean, first: Boolean = false) {
        if (!first) append(',')
        append('"')
        append(name)
        append("\":")
        append(value)
    }

    private fun StringBuilder.appendJsonArray(values: Set<String>) {
        append('[')
        values.asSequence().sorted().forEachIndexed { index, value ->
            if (index > 0) append(',')
            append('"')
            append(escapeJsonString(value))
            append('"')
        }
        append(']')
    }
}

@CacheableTask
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

    @get:Internal
    abstract val androidComponentCatalogDirectory: DirectoryProperty

    @get:InputFiles
    @get:Optional
    @get:PathSensitive(PathSensitivity.RELATIVE)
    val androidComponentCatalogFiles: ConfigurableFileCollection = project.objects.fileCollection()

    @get:OutputFile
    abstract val classGraphOutputFile: RegularFileProperty

    @get:OutputFile
    abstract val diagnosticsOutputFile: RegularFileProperty

    @get:OutputFile
    abstract val androidComponentCatalogOutputFile: RegularFileProperty

    @TaskAction
    fun merge() {
        InstrumentationArtifactFiles.mergeJsonl(classGraphDirectory.orNull?.asFile, classGraphOutputFile.get().asFile)
        InstrumentationArtifactFiles.mergeJsonl(diagnosticsDirectory.orNull?.asFile, diagnosticsOutputFile.get().asFile)
        InstrumentationArtifactFiles.mergeJsonl(
            androidComponentCatalogDirectory.orNull?.asFile,
            androidComponentCatalogOutputFile.get().asFile,
        )
    }
}
