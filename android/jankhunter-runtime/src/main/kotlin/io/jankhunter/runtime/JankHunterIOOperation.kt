package io.jankhunter.runtime

/** Stable operation kinds for bounded manual disk/database/content tracing. */
enum class JankHunterIOOperation(
    internal val wireValue: Long,
) {
    FILE_READ(1L),
    FILE_WRITE(2L),
    FILE_SYNC(3L),
    DATABASE_READ(4L),
    DATABASE_WRITE(5L),
    CONTENT_READ(6L),
    CONTENT_WRITE(7L),
}
