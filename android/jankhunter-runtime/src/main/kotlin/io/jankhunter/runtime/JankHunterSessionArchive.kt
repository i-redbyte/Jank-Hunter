package io.jankhunter.runtime

/** Metadata for one successfully published transport archive from this export invocation. */
class JankHunterSessionArchive internal constructor(
    val archivePath: String,
    val sizeBytes: Long,
    val completedHeapDumpCount: Int,
) {
    /** At least one included HPROF passed stream framing and completion checks. */
    val containsCompletedHeapDump: Boolean get() = completedHeapDumpCount > 0
}
