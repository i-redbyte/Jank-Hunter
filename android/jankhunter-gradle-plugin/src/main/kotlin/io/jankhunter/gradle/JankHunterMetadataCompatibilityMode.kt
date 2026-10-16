package io.jankhunter.gradle

/** Build-only policy; it neither selects the application's Kotlin version nor changes runtime ABI. */
enum class JankHunterMetadataCompatibilityMode {
    COMPATIBILITY,
    STRICT;

    internal fun validate(status: KotlinMetadataAnalysisStatus, className: String, version: String) {
        check(this != STRICT || status == KotlinMetadataAnalysisStatus.READABLE || status == KotlinMetadataAnalysisStatus.ABSENT) {
            val message = "Jank Hunter cannot fully analyze Kotlin metadata $version in $className: $status. " +
                "Use a compatible reader/plugin or explicitly select COMPATIBILITY for bytecode-only fallback."
            // AGP wraps visitor exceptions and hides their cause without --stacktrace.
            org.gradle.api.logging.Logging.getLogger(JankHunterMetadataCompatibilityMode::class.java).error(message)
            message
        }
    }
}
