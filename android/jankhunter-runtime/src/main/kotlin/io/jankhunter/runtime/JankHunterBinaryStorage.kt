package io.jankhunter.runtime

interface JankHunterBinaryStorage {

    val fileSizeLimitBytes: Long

    val archivesSizeLimitBytes: Long

    fun openWriter(fileName: String): JankHunterBinaryWriter

    fun createArtifact(fileName: String): JankHunterBinaryArtifact

    fun cleanup(protectedPath: String? = null)

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
