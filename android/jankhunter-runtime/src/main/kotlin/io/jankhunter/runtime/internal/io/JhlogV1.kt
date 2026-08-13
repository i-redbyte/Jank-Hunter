package io.jankhunter.runtime.internal.io

internal object JhlogV1 {
    const val FORMAT_MARKER = 0x80
    const val FORMAT_MAJOR = 1
    const val FORMAT_MINOR = 0
    const val FORMAT_PREFIX_BYTES = 10
    const val SUPERBLOCK_BYTES = 256L
    const val LIVE_SUMMARY_BYTES = 256L

    const val FILE_HEADER_OFFSET = 16L
    const val SUPERBLOCK_A_OFFSET = 8L * 1024L
    const val SUPERBLOCK_B_OFFSET = SUPERBLOCK_A_OFFSET + SUPERBLOCK_BYTES
    const val LIVE_SUMMARY_A_OFFSET = SUPERBLOCK_B_OFFSET + SUPERBLOCK_BYTES
    const val LIVE_SUMMARY_B_OFFSET = LIVE_SUMMARY_A_OFFSET + LIVE_SUMMARY_BYTES
    const val HISTORY_OFFSET = 12L * 1024L
    const val HISTORY_MAX_BYTES = 64 * 1024
    const val ARENA_OFFSET = 80L * 1024L

    const val CHUNK_HEADER_BYTES = 40
    const val COMMIT_TRAILER_BYTES = 24
    const val MAX_FILE_HEADER_BYTES = 4 * 1024
    const val MIN_FILE_BYTES = 1024L * 1024L

    const val SUPERBLOCK_SCHEMA = 1
    const val CHUNK_FLAG_GZIP = 1 shl 0
    const val CHUNK_FLAG_FINAL = 1 shl 1

    val FILE_PREFIX = byteArrayOf(
        'J'.code.toByte(),
        'H'.code.toByte(),
        'L'.code.toByte(),
        'O'.code.toByte(),
        'G'.code.toByte(),
        '\r'.code.toByte(),
        '\n'.code.toByte(),
        FORMAT_MARKER.toByte(),
        FORMAT_MAJOR.toByte(),
        FORMAT_MINOR.toByte(),
    )
    val SUPERBLOCK_MAGIC = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'S'.code.toByte(), 'B'.code.toByte())
    val CHUNK_MAGIC = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'C'.code.toByte(), '1'.code.toByte())
    val COMMIT_MAGIC = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'C'.code.toByte(), 'M'.code.toByte())
}
