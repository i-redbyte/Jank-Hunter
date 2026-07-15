package io.jankhunter.runtime

interface JankHunterBinaryStorage {

    /**
     * Positive physical upper bound for every `.jhlog` opened by this storage.
     *
     * Jank Hunter always honors this value, including when the internal
     * `sessionLogSizeLimitEnabled` policy is disabled. Use [Long.MAX_VALUE] when the storage
     * does not impose an additional per-file limit.
     */
    val fileSizeLimitBytes: Long

    /** Archive-retention budget owned and enforced by the storage implementation. */
    val archivesSizeLimitBytes: Long

    /**
     * Opens [fileName] without truncating an existing object. A new writer must report zero
     * [JankHunterBinaryWriter.bytesWritten]; a non-zero value is treated as a name collision and
     * is closed without writing.
     */
    fun openWriter(fileName: String): JankHunterBinaryWriter

    fun createArtifact(fileName: String): JankHunterBinaryArtifact

    /** Removes closed artifacts while preserving every path or file name in [protectedPaths]. */
    fun cleanup(protectedPaths: Set<String> = emptySet())

    fun listFiles(): List<String>
}

interface JankHunterBinaryWriter {

    val path: String

    fun bytesWritten(): Long

    fun writeByte(byte: Byte)

    fun writeBytes(bytes: ByteArray, offset: Int = 0, length: Int = bytes.size)

    fun flush()

    fun close()
}

interface JankHunterBinaryArtifact {

    val path: String

    fun commit()

    fun abort()
}
