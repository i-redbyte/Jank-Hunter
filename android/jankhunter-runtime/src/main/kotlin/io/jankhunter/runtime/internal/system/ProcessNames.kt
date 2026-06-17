package io.jankhunter.runtime.internal.system

import android.app.ActivityManager
import android.app.Application
import android.content.Context
import android.os.Build
import android.os.Process

internal object ProcessNames {
    fun current(context: Context): String {
        if (Build.VERSION.SDK_INT >= 28) {
            Application.getProcessName()?.takeIf { it.isNotBlank() }?.let { return it }
        }
        val pid = Process.myPid()
        val activityManager = context.getSystemService(Context.ACTIVITY_SERVICE) as? ActivityManager
        val processName = activityManager
            ?.runningAppProcesses
            ?.firstOrNull { it.pid == pid }
            ?.processName
        return processName?.takeIf { it.isNotBlank() } ?: context.packageName
    }

    fun safeFileSuffix(processName: String?, packageName: String): String {
        val normalized = displayName(processName, packageName)
            .replace(SAFE_FILE_SUFFIX_UNSAFE_CHARS, "_")
            .trim('_', '.', '-')
            .take(80)
        return normalized.takeIf { it.isNotBlank() } ?: "unknown"
    }

    private fun displayName(processName: String?, packageName: String): String {
        val name = processName?.takeIf { it.isNotBlank() } ?: return "unknown"
        if (name == packageName) return "main"
        if (name.startsWith("$packageName:")) return name.removePrefix("$packageName:")
        return name
    }

    private val SAFE_FILE_SUFFIX_UNSAFE_CHARS = Regex("[^A-Za-z0-9._-]")
}
