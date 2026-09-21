package io.jankhunter.gradle

import java.nio.file.Files
import java.util.zip.ZipEntry
import java.util.zip.ZipOutputStream
import org.gradle.api.DefaultTask
import org.gradle.api.file.ConfigurableFileCollection
import org.gradle.api.file.Directory
import org.gradle.api.file.DirectoryProperty
import org.gradle.api.file.RegularFile
import org.gradle.api.file.RegularFileProperty
import org.gradle.api.provider.ListProperty
import org.gradle.api.provider.Property
import org.gradle.api.provider.SetProperty
import org.gradle.api.tasks.CacheableTask
import org.gradle.api.tasks.Classpath
import org.gradle.api.tasks.Input
import org.gradle.api.tasks.OutputDirectory
import org.gradle.api.tasks.OutputFile
import org.gradle.api.tasks.TaskAction
import org.objectweb.asm.Opcodes

/** Class producers and classpath producers are task inputs, never speculative transform side effects. */
@CacheableTask
abstract class InstrumentJankHunterLifecycleTask : DefaultTask() {
    @get:Classpath abstract val inputJars: ListProperty<RegularFile>
    @get:Classpath abstract val inputDirectories: ListProperty<Directory>
    @get:Classpath abstract val supportClasspath: ConfigurableFileCollection
    @get:Input abstract val includePackages: SetProperty<String>
    @get:Input abstract val excludePackages: SetProperty<String>
    @get:Input abstract val includeWholeApplication: Property<Boolean>
    @get:Input abstract val validateRuntimeAbi: Property<Boolean>
    @get:OutputFile abstract val outputFile: RegularFileProperty
    @get:OutputDirectory abstract val diagnosticsDirectory: DirectoryProperty

    @TaskAction
    fun instrument() {
        val scratch = Files.createTempDirectory(temporaryDir.toPath(), "lifecycle-").toFile()
        try {
            LifecycleClassIndex(scratch.resolve("metadata.mv")).use { index ->
                LifecycleClassSpool(scratch.resolve("classes.bin"), index).use { spool ->
                    supportClasspath.files.forEach { file ->
                        LifecycleClassInputs.forEach(file) { index.add(it, false) }
                    }
                    inputJars.get().forEach { LifecycleClassInputs.forEach(it.asFile, spool::add) }
                    inputDirectories.get().forEach { LifecycleClassInputs.forEach(it.asFile, spool::add) }
                    instrument(index, spool)
                }
            }
        } finally {
            if (!scratch.deleteRecursively()) logger.warn("Cannot remove lifecycle temporary storage {}", scratch)
        }
    }

    private fun instrument(index: LifecycleClassIndex, spool: LifecycleClassSpool) {
        val selected = index.newWorkMap<Boolean>()
        index.programClasses().filter { header ->
            header.access and Opcodes.ACC_INTERFACE == 0 &&
                "Lio/jankhunter/annotations/JankHunterIgnore;" !in header.annotations &&
                !DependencyInjectionClassMatcher.isGeneratedDiClass(header.name) &&
                InstrumentationMatcher.matchesNormalizedClassName(
                    InstrumentationPackages.normalizePackage(header.name.replace('/', '.')),
                    includePackages.get(), excludePackages.get(), includeWholeApplication.get(),
                ) && index.kind(header.name) != LifecycleTargetKind.NONE
        }.forEach { selected[it.name] = true; index.maintainBudget() }
        // A library may compile without the host's runtime dependency. The application
        // validates the complete program before packaging the generated ABI references.
        if (selected.isNotEmpty() && validateRuntimeAbi.get()) {
            LifecycleRuntimeAbi.validate(index)
        }
        val diagnostics = diagnosticsDirectory.get().asFile
        // Only this task owns this directory. Removed classes must not leave stale diagnostic shards.
        if (diagnostics.exists()) check(diagnostics.deleteRecursively()) { "Cannot clear lifecycle diagnostics $diagnostics" }
        check(diagnostics.mkdirs() || diagnostics.isDirectory) { "Cannot create lifecycle diagnostics $diagnostics" }
        val ancestorPolicy = LifecycleAncestorAccessPolicy(excludePackages.get())
        val transformer = LifecycleScopedTransformer(index, selected.keys, diagnostics.absolutePath, ancestorPolicy::allows) {
            logger.warn("Jank Hunter: {}", it)
        }
        val output = outputFile.get().asFile
        output.parentFile.mkdirs()
        ZipOutputStream(output.outputStream().buffered()).use { sink ->
            index.programClasses().forEach { header ->
                sink.putNextEntry(ZipEntry(header.name + ".class").apply { time = 0L })
                sink.write(transformer.transform(spool.read(header.name)))
                sink.closeEntry()
                index.maintainBudget()
            }
        }
    }
}
