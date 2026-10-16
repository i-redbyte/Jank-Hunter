package io.jankhunter.gradle

import java.io.File
import java.nio.file.Files
import java.util.TreeMap
import java.util.zip.ZipEntry
import java.util.zip.ZipFile
import java.util.zip.ZipOutputStream
import org.gradle.api.DefaultTask
import org.gradle.api.file.Directory
import org.gradle.api.file.RegularFile
import org.gradle.api.file.RegularFileProperty
import org.gradle.api.provider.ListProperty
import org.gradle.api.tasks.CacheableTask
import org.gradle.api.tasks.Classpath
import org.gradle.api.tasks.OutputFile
import org.gradle.api.tasks.TaskAction

/** Captures the app module's exact classes before dependency-scoped instrumentation. */
@CacheableTask
abstract class CausalProvenancePrepassTask : DefaultTask() {
    @get:Classpath abstract val inputJars: ListProperty<RegularFile>
    @get:Classpath abstract val inputDirectories: ListProperty<Directory>
    @get:OutputFile abstract val outputFile: RegularFileProperty
    @get:OutputFile abstract val projectClasses: RegularFileProperty

    @TaskAction
    fun copyAndCatalog() {
        val opened = mutableListOf<ZipFile>()
        try {
            val entries = TreeMap<String, EntrySource>()
            inputJars.get().forEach { input ->
                val archive = ZipFile(input.asFile)
                opened.add(archive)
                archive.entries().asSequence().filterNot(ZipEntry::isDirectory).forEach { entry ->
                    add(entries, entry.name, EntrySource.Archive(archive, entry))
                }
            }
            inputDirectories.get().forEach { input ->
                val root = input.asFile.toPath()
                Files.walk(root).use { paths ->
                    paths.filter(Files::isRegularFile).forEach { file ->
                        val name = root.relativize(file).toString().replace(File.separatorChar, '/')
                        add(entries, name, EntrySource.Disk(file.toFile()))
                    }
                }
            }

            val copied = outputFile.get().asFile
            copied.parentFile.mkdirs()
            ZipOutputStream(copied.outputStream().buffered()).use { output ->
                entries.forEach { (name, source) ->
                    output.putNextEntry(ZipEntry(name).apply { time = 0L })
                    source.open().use { it.copyTo(output) }
                    output.closeEntry()
                }
            }
            val catalog = projectClasses.get().asFile
            catalog.parentFile.mkdirs()
            catalog.bufferedWriter().use { output ->
                entries.keys.asSequence().filter(::isProgramClass).forEach { name ->
                    output.appendLine(name.removeSuffix(".class"))
                }
            }
        } finally {
            opened.forEach(ZipFile::close)
        }
    }

    private fun add(entries: MutableMap<String, EntrySource>, name: String, source: EntrySource) {
        check(!name.startsWith('/') && name.split('/').none { it == ".." }) {
            "Invalid project class entry '$name'"
        }
        check(entries.putIfAbsent(name, source) == null) {
            "Duplicate project class entry '$name'; Jank Hunter cannot assign exact code origin"
        }
    }

    private sealed interface EntrySource {
        fun open(): java.io.InputStream

        class Disk(private val file: File) : EntrySource {
            override fun open(): java.io.InputStream = file.inputStream()
        }

        class Archive(private val archive: ZipFile, private val entry: ZipEntry) : EntrySource {
            override fun open(): java.io.InputStream = archive.getInputStream(entry)
        }
    }
}

internal fun isProgramClass(path: String): Boolean = path.endsWith(".class") &&
    path != "module-info.class" && !path.startsWith("META-INF/")
