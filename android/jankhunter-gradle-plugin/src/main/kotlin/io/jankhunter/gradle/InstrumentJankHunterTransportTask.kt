package io.jankhunter.gradle

import java.util.zip.ZipEntry
import java.util.zip.ZipFile
import java.util.zip.ZipOutputStream
import org.gradle.api.DefaultTask
import org.gradle.api.file.Directory
import org.gradle.api.file.RegularFile
import org.gradle.api.file.RegularFileProperty
import org.gradle.api.provider.ListProperty
import org.gradle.api.provider.SetProperty
import org.gradle.api.tasks.CacheableTask
import org.gradle.api.tasks.Classpath
import org.gradle.api.tasks.Input
import org.gradle.api.tasks.InputFile
import org.gradle.api.tasks.OutputFile
import org.gradle.api.tasks.PathSensitive
import org.gradle.api.tasks.PathSensitivity
import org.gradle.api.tasks.TaskAction
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassWriter

/** A task boundary is required: dependency artifact transforms may run ahead of producer tasks. */
@CacheableTask
abstract class InstrumentJankHunterTransportTask : DefaultTask() {
    @get:Classpath abstract val inputJars: ListProperty<RegularFile>
    @get:Classpath abstract val inputDirectories: ListProperty<Directory>
    @get:Input abstract val excludePackages: SetProperty<String>
    @get:InputFile
    @get:PathSensitive(PathSensitivity.NONE)
    abstract val capabilityFile: RegularFileProperty
    @get:OutputFile abstract val outputFile: RegularFileProperty

    @TaskAction
    fun instrument() {
        val enabled = TransportCapability.decode(capabilityFile.get().asFile.readText()) == TransportCapability.AVAILABLE
        val output = outputFile.get().asFile
        output.parentFile.mkdirs()
        ZipOutputStream(output.outputStream().buffered()).use { sink ->
            inputJars.get().forEach { jar ->
                ZipFile(jar.asFile).use { source ->
                    source.entries().asSequence().filter { isClass(it.name) }.sortedBy { it.name }.forEach { entry ->
                        source.getInputStream(entry).use { input ->
                            writeClass(sink, entry.name, input, enabled)
                        }
                    }
                }
            }
            inputDirectories.get().forEach { directory ->
                directory.asFile.walkTopDown().filter { it.isFile && it.extension == "class" }
                    .sortedBy { it.relativeTo(directory.asFile).invariantSeparatorsPath }.forEach { file ->
                    val name = file.relativeTo(directory.asFile).invariantSeparatorsPath
                    if (isClass(name)) file.inputStream().use { writeClass(sink, name, it, enabled) }
                }
            }
        }
    }

    private fun writeClass(sink: ZipOutputStream, name: String, input: java.io.InputStream, enabled: Boolean) {
        sink.putNextEntry(ZipEntry(name).apply { time = 0L })
        val className = name.removeSuffix(".class").replace('/', '.')
        val excluded = excludePackages.get().any { InstrumentationPackages.matchesPackageBoundary(className, it) }
        if (enabled && !excluded && OkHttpTransportClassVisitor.matches(className)) {
            // Existing stack-map frames remain valid: the visitor adds no branch targets,
            // changes no locals, and preserves the stack at every original instruction boundary.
            val reader = ClassReader(input)
            val writer = ClassWriter(reader, ClassWriter.COMPUTE_MAXS)
            reader.accept(OkHttpTransportClassVisitor(writer), 0)
            sink.write(writer.toByteArray())
        } else {
            input.copyTo(sink)
        }
        sink.closeEntry()
    }

    // Java resources have a separate AGP artifact pipeline. Multi-release JVM metadata
    // is not part of Android program classes; module-info is likewise not an Android class.
    private fun isClass(name: String): Boolean = name.endsWith(".class") &&
        name != "module-info.class" && !name.startsWith("META-INF/")
}
