package io.jankhunter.runtime.internal.system

import android.app.ActivityManager
import android.app.Application
import android.content.Context
import android.content.pm.ComponentInfo
import android.content.pm.PackageManager
import android.os.Build
import android.os.Process

internal object ProcessNames {
    data class Roster(val names: Set<String>, val declarationComplete: Boolean)

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

    fun declared(context: Context): Roster {
        val names = linkedSetOf(context.packageName)
        return try {
            val flags = PackageManager.GET_ACTIVITIES or PackageManager.GET_SERVICES or
                PackageManager.GET_RECEIVERS or PackageManager.GET_PROVIDERS or
                PackageManager.MATCH_DISABLED_COMPONENTS
            val info = if (Build.VERSION.SDK_INT >= 33) {
                context.packageManager.getPackageInfo(
                    context.packageName,
                    PackageManager.PackageInfoFlags.of(flags.toLong()),
                )
            } else {
                context.packageManager.getPackageInfo(context.packageName, flags)
            }
            info.applicationInfo?.processName?.takeIf(String::isNotBlank)?.let(names::add)
            fun addProcesses(components: Array<out ComponentInfo>?) {
                components?.forEach { component ->
                    component.processName?.takeIf(String::isNotBlank)?.let(names::add)
                }
            }
            addProcesses(info.activities)
            addProcesses(info.services)
            addProcesses(info.receivers)
            addProcesses(info.providers)
            Roster(names, declarationComplete = true)
        } catch (_: PackageManager.NameNotFoundException) {
            Roster(names, declarationComplete = false)
        } catch (_: RuntimeException) {
            Roster(names, declarationComplete = false)
        }
    }
}
