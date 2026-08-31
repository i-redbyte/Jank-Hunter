package io.jankhunter.runtime

enum class JankHunterOperationOutcome(internal val wireValue: Long) {
    SUCCESS(1L),
    FAILURE(2L),
    CANCELLED(3L),
    TIMEOUT(4L),
}
