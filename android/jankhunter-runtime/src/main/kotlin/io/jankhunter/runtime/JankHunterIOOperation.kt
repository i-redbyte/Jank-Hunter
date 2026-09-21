package io.jankhunter.runtime

/** Stable operation kinds for bounded manual disk/content tracing. */
enum class JankHunterIOOperation(
    internal val wireValue: Long,
) {
    FILE_READ(1L),
    FILE_WRITE(2L),
    FILE_SYNC(3L),
    CONTENT_READ(6L),
    CONTENT_WRITE(7L),
}
