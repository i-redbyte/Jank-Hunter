package io.jankhunter.runtime

import java.io.File
import java.util.Collections

/**
 * Immutable host storage contract. Limits use bytes without an implicit MB/MiB conversion.
 * Extension sets contain the host's effective normalized values and are copied verbatim.
 */
class JankHunterStoragePolicy(
    rootDirectory: File,
    val fileSizeLimitBytes: Long,
    val archivesSizeLimitBytes: Long,
    fileExtensions: Set<String>,
    artifactFileSizeLimitExemptExtensions: Set<String>,
    val bufferSize: Int,
    val handleDiskFull: Boolean,
) {
    val rootDirectory: File = rootDirectory.canonicalFile
    val fileExtensions: Set<String> = freezeExtensions(fileExtensions)
    val artifactFileSizeLimitExemptExtensions: Set<String> = freezeExtensions(artifactFileSizeLimitExemptExtensions)

    init {
        require(fileSizeLimitBytes > 0L) { "file size limit must be positive" }
        require(archivesSizeLimitBytes > 0L) { "archive size limit must be positive" }
        require(bufferSize > 0) { "binary buffer size must be positive" }
    }

    fun allowsExtension(extension: String): Boolean = extension in fileExtensions

    fun fileLimitBytes(extension: String): Long =
        if (extension in artifactFileSizeLimitExemptExtensions) Long.MAX_VALUE else fileSizeLimitBytes

    override fun equals(other: Any?): Boolean = other is JankHunterStoragePolicy &&
        rootDirectory == other.rootDirectory && fileSizeLimitBytes == other.fileSizeLimitBytes &&
        archivesSizeLimitBytes == other.archivesSizeLimitBytes && fileExtensions == other.fileExtensions &&
        artifactFileSizeLimitExemptExtensions == other.artifactFileSizeLimitExemptExtensions &&
        bufferSize == other.bufferSize && handleDiskFull == other.handleDiskFull

    override fun hashCode(): Int {
        var result = rootDirectory.hashCode()
        result = 31 * result + fileSizeLimitBytes.hashCode()
        result = 31 * result + archivesSizeLimitBytes.hashCode()
        result = 31 * result + fileExtensions.hashCode()
        result = 31 * result + artifactFileSizeLimitExemptExtensions.hashCode()
        result = 31 * result + bufferSize
        return 31 * result + handleDiskFull.hashCode()
    }

    private fun freezeExtensions(values: Set<String>): Set<String> = Collections.unmodifiableSet(
        values.mapTo(LinkedHashSet(values.size)) { value ->
            value.also { extension ->
                require(extension.isNotEmpty() && extension.none { it == '/' || it == '\\' || it.isISOControl() }) {
                    "invalid artifact extension"
                }
            }
        },
    )
}
