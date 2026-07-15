package io.jankhunter.gradle

import org.gradle.api.DefaultTask
import org.gradle.api.file.ConfigurableFileCollection
import org.gradle.api.file.DirectoryProperty
import org.gradle.api.file.RegularFileProperty
import org.gradle.api.provider.Property
import org.gradle.api.tasks.Input
import org.gradle.api.tasks.InputFiles
import org.gradle.api.tasks.Internal
import org.gradle.api.tasks.Optional
import org.gradle.api.tasks.OutputFile
import org.gradle.api.tasks.PathSensitive
import org.gradle.api.tasks.PathSensitivity
import org.gradle.api.tasks.TaskAction

abstract class MergeJankHunterDependencyInjectionCatalogTask : DefaultTask() {
    @get:Input
    abstract val analysisEnabled: Property<Boolean>

    @get:Input
    abstract val variantName: Property<String>

    @get:Internal
    abstract val shardsDirectory: DirectoryProperty

    @get:InputFiles
    @get:Optional
    @get:PathSensitive(PathSensitivity.RELATIVE)
    val shardFiles: ConfigurableFileCollection = project.objects.fileCollection()

    @get:OutputFile
    abstract val outputFile: RegularFileProperty

    @TaskAction
    fun merge() {
        val output = outputFile.get().asFile
        if (!analysisEnabled.getOrElse(false)) {
            output.delete()
            shardsDirectory.orNull?.asFile?.deleteRecursively()
            return
        }
        val records = shardsDirectory.orNull?.asFile
            ?.let(InstrumentationArtifactFiles::readJsonlLines)
            .orEmpty()
            .asSequence()
            .distinct()
            .sorted()
            .toList()
        val lines = buildList {
            add(DependencyInjectionCatalogWriter.metadataRecord(variantName.get()))
            addAll(records)
        }
        InstrumentationArtifactFiles.writeAtomically(output, lines.joinToString(separator = "\n", postfix = "\n"))
    }
}
