package io.jankhunter.gradle

import java.io.File
import java.util.zip.ZipFile
import org.gradle.api.DefaultTask
import org.gradle.api.file.ConfigurableFileCollection
import org.gradle.api.file.DirectoryProperty
import org.gradle.api.file.RegularFileProperty
import org.gradle.api.provider.Property
import org.gradle.api.tasks.CacheableTask
import org.gradle.api.tasks.Input
import org.gradle.api.tasks.InputDirectory
import org.gradle.api.tasks.InputFile
import org.gradle.api.tasks.InputFiles
import org.gradle.api.tasks.Optional
import org.gradle.api.tasks.OutputFile
import org.gradle.api.tasks.PathSensitive
import org.gradle.api.tasks.PathSensitivity
import org.gradle.api.tasks.TaskAction

/** Post-packaging evidence; never embedded back into the package whose hash it records. */
@CacheableTask
abstract class GenerateJankHunterBuildManifestTask : DefaultTask() {
    @get:Input abstract val variantName: Property<String>
    @get:Input abstract val packageKind: Property<String>
    @get:Input abstract val symbolNamespace: Property<String>
    @get:InputDirectory @get:Optional @get:PathSensitive(PathSensitivity.RELATIVE) abstract val apkDirectory: DirectoryProperty
    @get:InputFile @get:Optional @get:PathSensitive(PathSensitivity.NAME_ONLY) abstract val bundleFile: RegularFileProperty
    @get:InputFiles @get:PathSensitive(PathSensitivity.NAME_ONLY) abstract val artifacts: ConfigurableFileCollection
    @get:InputFile @get:PathSensitive(PathSensitivity.NONE) abstract val identityAsset: RegularFileProperty
    @get:InputFile @get:Optional @get:PathSensitive(PathSensitivity.NONE) abstract val mappingFile: RegularFileProperty
    @get:OutputFile abstract val outputFile: RegularFileProperty

    @TaskAction
    fun generate() {
        val kind = packageKind.get()
        check(kind == "apk" || kind == "aab") { "Unsupported build manifest package kind $kind" }
        val identity = identityAsset.get().asFile.inputStream().use { it.readNBytes(MAX_IDENTITY_BYTES + 1) }
        check(identity.size <= MAX_IDENTITY_BYTES) { "Oversized build identity asset" }
        val mappingDigest = mappingFile.orNull?.asFile?.let(::artifactSha256)
        check(identity.contentEquals(buildIdentityAssetText(mappingDigest, symbolNamespace.get()).toByteArray(Charsets.UTF_8))) {
            "Build identity asset does not bind the supplied mapping and symbol namespace"
        }
        val packageFiles = if (kind == "apk") {
            apkDirectory.get().asFile.walkTopDown().filter { it.isFile && it.extension == kind }
                .take(MAX_PACKAGES + 1).toList()
        } else listOf(bundleFile.get().asFile)
        check(packageFiles.isNotEmpty() && packageFiles.size <= MAX_PACKAGES) { "Invalid build manifest package count" }
        check(packageFiles.map { it.name }.toSet().size == packageFiles.size) { "Duplicate package names in build manifest" }
        for (file in packageFiles) {
            ZipFile(file).use { zip ->
                val prefix = if (kind == "aab") "base/assets/" else "assets/"
                val entry = checkNotNull(zip.getEntry(prefix + GenerateJankHunterBuildIdentityTask.ASSET_PATH)) {
                    "Packaged build identity is missing: $file"
                }
                val actual = zip.getInputStream(entry).use { it.readNBytes(MAX_IDENTITY_BYTES + 1) }
                check(actual.contentEquals(identity)) { "Packaged build identity differs from mapping identity: $file" }
            }
        }
        val artifactFiles = artifacts.files.sortedBy { it.name }
        check(artifactFiles.size <= MAX_ARTIFACTS && artifactFiles.all { it.isFile }) { "Invalid build manifest artifacts" }
        check(artifactFiles.map { it.name }.toSet().size == artifactFiles.size) { "Duplicate artifact names in build manifest" }
        val text = buildString {
            append("{\"format\":1,\"kind\":\"build-manifest\",\"variant\":\"")
            append(escapeJsonString(variantName.get()))
            append("\",\"symbolNamespace\":\"").append(escapeJsonString(symbolNamespace.get()))
            append("\",\"mappingSha256\":\"").append(mappingDigest.orEmpty())
            append("\",\"identityAssetSha256\":\"").append(artifactSha256(identityAsset.get().asFile))
            append("\",\"packages\":").append(hashes(packageFiles.sortedBy { it.name }))
            append(",\"artifacts\":").append(hashes(artifactFiles)).append("}\n")
        }
        val output = outputFile.get().asFile
        check(output.parentFile.mkdirs() || output.parentFile.isDirectory) { "Cannot create build manifest directory" }
        output.writeText(text, Charsets.UTF_8)
    }

    private fun hashes(files: List<File>): String = files.joinToString(",", "[", "]") { file ->
        "{\"file\":\"${escapeJsonString(file.name)}\",\"sha256\":\"${artifactSha256(file)}\"}"
    }

    private companion object {
        const val MAX_IDENTITY_BYTES = 512
        const val MAX_PACKAGES = 1024
        const val MAX_ARTIFACTS = 32
    }
}
