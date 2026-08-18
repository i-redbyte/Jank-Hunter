package io.jankhunter.runtime

import java.util.Collections

/** Stable, sealed vector frontier captured across live app processes while collection continues. */
class JankHunterLogSnapshot internal constructor(
    val capturedAtMs: Long,
    logPaths: List<String>,
    val processCount: Int = 1,
    val captureSkewMs: Long = 0L,
) {
    val logPaths: List<String> = Collections.unmodifiableList(ArrayList(logPaths))
}
