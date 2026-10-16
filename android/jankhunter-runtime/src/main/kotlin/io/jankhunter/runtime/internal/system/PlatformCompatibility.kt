package io.jankhunter.runtime.internal.system

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.PackageInfo
import android.os.Build

internal fun packageVersionCode(info: PackageInfo): Long = if (Build.VERSION.SDK_INT >= 28) {
    info.longVersionCode
} else {
    // Retain legacy support without importing a newer AndroidX publication graph.
    PackageInfo::class.java.getField("versionCode").getInt(info).toLong()
}

internal fun registerPrivateReceiver(
    context: Context, receiver: BroadcastReceiver, filter: IntentFilter, signaturePermission: String,
): Intent? = if (Build.VERSION.SDK_INT >= 33) {
    context.registerReceiver(receiver, filter, signaturePermission, null, Context.RECEIVER_NOT_EXPORTED)
} else {
    // The runtime manifest declares this application-specific permission as signature.
    context.registerReceiver(receiver, filter, signaturePermission, null)
}
