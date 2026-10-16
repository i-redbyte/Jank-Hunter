package io.jankhunter.runtime.internal.io

import java.io.File

/** Component-boundary comparison for already canonical roots, also available before Android 26. */
internal fun isWithinArtifactRoot(candidate: File, root: File): Boolean {
    val prefix = root.path.trimEnd(File.separatorChar) + File.separator
    return candidate == root || candidate.path.startsWith(prefix)
}
