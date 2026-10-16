package io.jankhunter.gradle

import java.io.File
import java.nio.file.Files
import java.util.zip.ZipFile
import org.gradle.api.DefaultTask
import org.gradle.api.file.ConfigurableFileCollection
import org.gradle.api.file.RegularFileProperty
import org.gradle.api.tasks.CacheableTask
import org.gradle.api.tasks.Classpath
import org.gradle.api.tasks.InputFile
import org.gradle.api.tasks.OutputFile
import org.gradle.api.tasks.PathSensitive
import org.gradle.api.tasks.PathSensitivity
import org.gradle.api.tasks.TaskAction

internal enum class CausalCodeOrigin(val wireValue: Int) {
    UNKNOWN(0), PROJECT(1), DEPENDENCY(2),
}

/** A deterministic class-level build-origin catalogue, independent of Java package names. */
internal class CausalCodeOriginCatalog private constructor(private val byClass: Map<String, CausalCodeOrigin>) {
    fun origin(internalClassName: String): CausalCodeOrigin =
        byClass[internalClassName.replace('.', '/')] ?: CausalCodeOrigin.UNKNOWN

    companion object {
        val EMPTY = CausalCodeOriginCatalog(emptyMap())
        private const val HEADER = "JHCORIGIN1"
        fun load(file: File): CausalCodeOriginCatalog = parse(file.readText())

        fun parse(content: String): CausalCodeOriginCatalog {
            val classes = HashMap<String, CausalCodeOrigin>()
            content.reader().buffered().use { input ->
                require(input.readLine() == HEADER) { "Unsupported Jank Hunter code-origin catalog" }
                input.lineSequence().forEach { line ->
                    val separator = line.indexOf('\t')
                    require(separator == 1 && line.length > 2) { "Malformed Jank Hunter code-origin entry" }
                    val origin = when (line[0]) {
                        '1' -> CausalCodeOrigin.PROJECT
                        '2' -> CausalCodeOrigin.DEPENDENCY
                        '0' -> CausalCodeOrigin.UNKNOWN
                        else -> error("Unknown Jank Hunter code origin")
                    }
                    require(classes.putIfAbsent(line.substring(2), origin) == null) {
                        "Duplicate Jank Hunter code-origin entry"
                    }
                }
            }
            return CausalCodeOriginCatalog(classes)
        }

        internal fun header(): String = HEADER
    }
}

@CacheableTask
abstract class GenerateCausalCodeOriginCatalogTask : DefaultTask() {
    @get:InputFile
    @get:PathSensitive(PathSensitivity.NONE)
    abstract val projectClasses: RegularFileProperty

    @get:Classpath abstract val localProjectArtifacts: ConfigurableFileCollection
    @get:Classpath abstract val externalArtifacts: ConfigurableFileCollection
    @get:Classpath abstract val unknownArtifacts: ConfigurableFileCollection
    @get:OutputFile abstract val outputFile: RegularFileProperty

    @TaskAction
    fun generate() {
        val origins = HashMap<String, CausalCodeOrigin>()
        projectClasses.get().asFile.useLines { lines ->
            lines.forEach { merge(origins, it, CausalCodeOrigin.PROJECT) }
        }
        for (file in localProjectArtifacts.files) scan(file) { merge(origins, it, CausalCodeOrigin.PROJECT) }
        for (file in externalArtifacts.files) scan(file) { merge(origins, it, CausalCodeOrigin.DEPENDENCY) }
        for (file in unknownArtifacts.files) scan(file) { merge(origins, it, CausalCodeOrigin.UNKNOWN) }

        val target = outputFile.get().asFile
        target.parentFile.mkdirs()
        target.bufferedWriter().use { output ->
            output.appendLine(CausalCodeOriginCatalog.header())
            origins.toSortedMap().forEach { (name, origin) ->
                output.append(origin.wireValue.toString()).append('\t').append(name).append('\n')
            }
        }
    }

    private fun merge(target: MutableMap<String, CausalCodeOrigin>, name: String, origin: CausalCodeOrigin) {
        if (name.isBlank()) return
        val previous = target.putIfAbsent(name, origin)
        if (previous != null) target[name] = CausalCodeOrigin.UNKNOWN
    }

    private fun scan(file: File, consume: (String) -> Unit) {
        if (file.isDirectory) {
            val root = file.toPath()
            Files.walk(root).use { paths ->
                paths.filter { Files.isRegularFile(it) }.forEach { path ->
                    val name = root.relativize(path).toString().replace(File.separatorChar, '/')
                    if (isProgramClass(name)) consume(name.removeSuffix(".class"))
                }
            }
        } else {
            ZipFile(file).use { archive ->
                archive.entries().asSequence().forEach { entry ->
                    if (!entry.isDirectory && isProgramClass(entry.name)) consume(entry.name.removeSuffix(".class"))
                }
            }
        }
    }
}
