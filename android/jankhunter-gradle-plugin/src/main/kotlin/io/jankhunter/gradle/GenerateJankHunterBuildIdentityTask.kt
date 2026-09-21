package io.jankhunter.gradle

import java.io.File
import java.security.MessageDigest
import org.gradle.api.DefaultTask
import org.gradle.api.file.DirectoryProperty
import org.gradle.api.file.RegularFileProperty
import org.gradle.api.provider.Property
import org.gradle.api.tasks.CacheableTask
import org.gradle.api.tasks.Input
import org.gradle.api.tasks.InputFile
import org.gradle.api.tasks.Optional
import org.gradle.api.tasks.OutputDirectory
import org.gradle.api.tasks.PathSensitive
import org.gradle.api.tasks.PathSensitivity
import org.gradle.api.tasks.TaskAction

/** Reads the completed R8 mapping without feeding its digest back into bytecode. */
@CacheableTask
abstract class GenerateJankHunterBuildIdentityTask : DefaultTask() {
    @get:Input abstract val minified: Property<Boolean>
    @get:Input abstract val symbolNamespace: Property<String>
    @get:InputFile @get:Optional @get:PathSensitive(PathSensitivity.NONE)
    abstract val mappingFile: RegularFileProperty
    @get:OutputDirectory abstract val assetsDirectory: DirectoryProperty

    @TaskAction
    fun generate() {
        check(minified.get() == mappingFile.isPresent) { "Minified build identity requires exactly one R8 mapping input" }
        val namespace = symbolNamespace.get()
        check(namespace.matches(Regex("[0-9a-f]{32}"))) { "Invalid build identity symbol namespace" }
        val mapping = mappingFile.orNull?.asFile
        val digest = mapping?.let { file ->
            check(file.length() in 1..MAX_MAPPING_BYTES) { "Invalid mapping size for build identity: $file" }
            artifactSha256(file)
        }.orEmpty()
        val target = assetsDirectory.file(ASSET_PATH).get().asFile
        check(target.parentFile.mkdirs() || target.parentFile.isDirectory) { "Cannot create build identity asset directory" }
        target.writeText(buildIdentityAssetText(if (mapping == null) null else digest, namespace), Charsets.UTF_8)
    }

    companion object {
        const val ASSET_PATH = "jankhunter/build-identity-v1.txt"
        private const val MAX_MAPPING_BYTES = 1024L * 1024 * 1024
    }
}

internal fun artifactSha256(file: File): String {
    val digest = MessageDigest.getInstance("SHA-256")
    file.inputStream().buffered().use { input ->
        val buffer = ByteArray(64 * 1024)
        while (true) {
            val count = input.read(buffer)
            if (count < 0) break
            digest.update(buffer, 0, count)
        }
    }
    return digest.digest().joinToString("") { byte -> "%02x".format(byte.toInt() and 0xff) }
}

internal fun buildIdentityAssetText(mappingDigest: String?, namespace: String): String =
    "schema=1\nstate=${if (mappingDigest == null) "unminified" else "mapped"}\n" +
        "mapping-sha256=${mappingDigest.orEmpty()}\nsymbol-namespace=$namespace\n"
