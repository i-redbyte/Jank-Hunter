package io.jankhunter.gradle

import java.nio.file.Files
import org.gradle.api.DefaultTask
import org.gradle.api.file.Directory
import org.gradle.api.file.DirectoryProperty
import org.gradle.api.file.RegularFile
import org.gradle.api.provider.ListProperty
import org.gradle.api.provider.Property
import org.gradle.api.tasks.CacheableTask
import org.gradle.api.tasks.Classpath
import org.gradle.api.tasks.Input
import org.gradle.api.tasks.OutputDirectory
import org.gradle.api.tasks.TaskAction

@CacheableTask
abstract class GenerateJankHunterWorkerCatalogTask : DefaultTask() {
    @get:Classpath abstract val inputJars: ListProperty<RegularFile>
    @get:Classpath abstract val inputDirectories: ListProperty<Directory>
    @get:Input abstract val symbolNamespace: Property<String>
    @get:OutputDirectory abstract val assetsDirectory: DirectoryProperty

    @TaskAction
    fun generate() {
        val scratch = Files.createTempDirectory(temporaryDir.toPath(), "worker-catalog-").toFile()
        try {
            LifecycleClassIndex(scratch.resolve("metadata.mv")).use { index ->
                inputJars.get().forEach { input -> LifecycleClassInputs.forEach(input.asFile) { index.add(it, true) } }
                inputDirectories.get().forEach { input -> LifecycleClassInputs.forEach(input.asFile) { index.add(it, true) } }
                val target = assetsDirectory.file(ASSET_PATH).get().asFile
                check(target.parentFile.mkdirs() || target.parentFile.isDirectory) { "Cannot create worker catalog directory" }
                target.writeText(workerCatalog(index, MAX_WORKERS).assetText(symbolNamespace.get()), Charsets.UTF_8)
            }
        } finally {
            if (!scratch.deleteRecursively()) logger.warn("Cannot remove worker catalog temporary storage {}", scratch)
        }
    }

    companion object {
        const val ASSET_PATH = "jankhunter/worker-catalog-v1.json"
        private const val MAX_WORKERS = 4096
    }
}
