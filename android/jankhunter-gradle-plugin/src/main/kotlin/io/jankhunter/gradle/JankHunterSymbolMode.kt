package io.jankhunter.gradle

/** Controls how ASM method symbols are stored in a .jhlog file. */
enum class JankHunterSymbolMode {
    /** Store readable method symbols in the log so any matching CLI installation can analyze it. */
    EMBEDDED,

    /** Keep only stable IDs in the log and resolve them from Gradle owner-map artifacts in the CLI. */
    STABLE_EXTERNAL,
}
