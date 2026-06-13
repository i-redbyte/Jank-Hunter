package io.jankhunter.runtime.internal.system

import android.os.Build

internal data class DeviceSnapshot(
    val displayName: String,
    val androidRelease: String,
    val securityPatch: String,
    val primaryAbi: String,
    val supportedAbis: String,
    val manufacturer: String,
    val brand: String,
    val hardware: String,
    val board: String,
    val product: String,
)

internal object DeviceSnapshots {
    @Volatile
    private var cached: DeviceSnapshot? = null

    fun current(): DeviceSnapshot {
        cached?.let { return it }
        return synchronized(this) {
            cached ?: read().also { cached = it }
        }
    }

    private fun read(): DeviceSnapshot {
        val manufacturer = Build.MANUFACTURER.clean()
        val model = Build.MODEL.clean()
        val displayName = listOf(manufacturer, model)
            .filter { it.isNotBlank() && it != "unknown" }
            .joinToString(" ")
            .ifBlank { "unknown" }
        val abis = Build.SUPPORTED_ABIS
            .filter { it.isNotBlank() }
            .joinToString(",")
            .ifBlank { "unknown" }
        return DeviceSnapshot(
            displayName = displayName,
            androidRelease = Build.VERSION.RELEASE.clean(),
            securityPatch = Build.VERSION.SECURITY_PATCH.clean(),
            primaryAbi = Build.SUPPORTED_ABIS.firstOrNull().clean(),
            supportedAbis = abis,
            manufacturer = manufacturer,
            brand = Build.BRAND.clean(),
            hardware = Build.HARDWARE.clean(),
            board = Build.BOARD.clean(),
            product = Build.PRODUCT.clean(),
        )
    }

    private fun String?.clean(): String {
        return this?.trim()?.takeIf { it.isNotEmpty() } ?: "unknown"
    }
}
