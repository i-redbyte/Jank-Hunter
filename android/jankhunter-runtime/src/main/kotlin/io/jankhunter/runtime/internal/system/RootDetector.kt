package io.jankhunter.runtime.internal.system

import android.os.Build
import java.io.File

internal object RootDetector {
    private val rootMarkerPaths = arrayOf(
        "/system/bin/su",
        "/system/xbin/su",
        "/sbin/su",
        "/vendor/bin/su",
        "/su/bin/su",
        "/system/bin/magisk",
        "/sbin/magisk",
        "/debug_ramdisk/magisk",
        "/system/app/Superuser.apk",
    )

    fun isLikelyRooted(
        buildTags: String? = Build.TAGS,
        fileExists: (String) -> Boolean = { path -> File(path).exists() },
    ): Boolean {
        if (buildTags?.contains("test-keys", ignoreCase = true) == true) {
            return true
        }
        // Deliberately avoid shell commands and package scans: startup must stay cheap and non-blocking.
        return rootMarkerPaths.any { path ->
            try {
                fileExists(path)
            } catch (_: Exception) {
                false
            }
        }
    }
}
