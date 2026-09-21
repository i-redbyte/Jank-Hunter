package io.jankhunter.gradle

import org.gradle.api.DefaultTask
import org.gradle.api.file.ConfigurableFileCollection
import org.gradle.api.file.DirectoryProperty
import org.gradle.api.provider.Property
import org.gradle.api.tasks.CacheableTask
import org.gradle.api.tasks.Input
import org.gradle.api.tasks.InputFiles
import org.gradle.api.tasks.Optional
import org.gradle.api.tasks.OutputDirectory
import org.gradle.api.tasks.PathSensitive
import org.gradle.api.tasks.PathSensitivity
import org.gradle.api.tasks.TaskAction

@CacheableTask
abstract class GenerateJankHunterArtTiScaledConfigTask : DefaultTask() {
    @get:Input
    abstract val artTiConfigBlob: Property<String>

    @get:Input
    abstract val artTiExplicitOverridesBlob: Property<String>

    @get:Input
    abstract val artTiScaleToApplicationSize: Property<Boolean>

    @get:Input
    abstract val artTiStorageLimitMiB: Property<Int>

    @get:Input
    abstract val artTiGradleModuleCount: Property<Int>

    @get:InputFiles
    @get:Optional
    @get:PathSensitive(PathSensitivity.RELATIVE)
    val diagnosticsFiles: ConfigurableFileCollection = project.objects.fileCollection()

    @get:OutputDirectory
    abstract val assetsDirectory: DirectoryProperty

    init {
        artTiConfigBlob.convention("")
        artTiExplicitOverridesBlob.convention("")
        artTiScaleToApplicationSize.convention(true)
        artTiStorageLimitMiB.convention(50)
        artTiGradleModuleCount.convention(1)
    }

    @TaskAction
    fun writeAsset() {
        val blob = artTiConfigBlob.get()
        if (blob.isEmpty()) return
        val (nativeOptions, triggerPolicy) = ArtTiResolvedMetadata.resolve(
            configBlob = blob,
            overridesBlob = artTiExplicitOverridesBlob.get(),
            diagnosticsFiles = diagnosticsFiles.files,
            storageLimitMiB = artTiStorageLimitMiB.get(),
            scaleToApplicationSize = artTiScaleToApplicationSize.get(),
            gradleModuleCount = artTiGradleModuleCount.get(),
        )
        val asset = assetsDirectory.get().file(ASSET_PATH).asFile
        asset.parentFile.mkdirs()
        asset.writeText("$nativeOptions\n$triggerPolicy\n")
    }

    companion object {
        const val ASSET_PATH = "jankhunter/artti-runtime-config.txt"
    }
}
