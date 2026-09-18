package io.jankhunter.gradle

import java.io.File
import java.util.zip.ZipFile
import org.gradle.api.DefaultTask
import org.gradle.api.file.ConfigurableFileCollection
import org.gradle.api.file.RegularFileProperty
import org.gradle.api.tasks.CacheableTask
import org.gradle.api.tasks.Classpath
import org.gradle.api.tasks.OutputFile
import org.gradle.api.tasks.TaskAction

@CacheableTask
abstract class ResolveJankHunterTransportCapabilityTask : DefaultTask() {
    @get:Classpath
    abstract val supportClasspath: ConfigurableFileCollection

    @get:OutputFile
    abstract val outputFile: RegularFileProperty

    @TaskAction
    fun resolve() {
        val capability = when {
            !containsClass(OkHttpTransportClassVisitor.OWNER_ABI) -> TransportCapability.MISSING_HELPER
            !containsClass("okhttp3/internal/http1/Http1Codec") -> TransportCapability.UNSUPPORTED_CODEC
            else -> TransportCapability.AVAILABLE
        }
        InstrumentationArtifactFiles.writeAtomically(outputFile.get().asFile, capability.encoded)
    }
    private fun containsClass(className: String): Boolean = supportClasspath.any { file ->
        containsClass(file, className)
    }

    private fun containsClass(file: File, className: String): Boolean {
        check(file.exists()) { "Transport capability input was not built: $file" }
        val entry = "$className.class"
        return when {
            file.isDirectory -> File(file, entry).isFile
            file.extension == "jar" -> ZipFile(file).use { it.getEntry(entry) != null }
            else -> false
        }
    }
}
