package io.jankhunter.runtime

enum class JankHunterOperationKind(internal val wireValue: Long) {
    USER(1L),
    SCREEN(2L),
    BACKGROUND(3L),
    SYSTEM(4L),
    STAGE(5L),
}
