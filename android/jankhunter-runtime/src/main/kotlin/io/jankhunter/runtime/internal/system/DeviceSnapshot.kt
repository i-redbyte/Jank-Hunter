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
    val rooted: Boolean,
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
        // Android declares this array non-null, but vendor/platform stubs can still return null.
        // Metadata collection must never prevent the runtime writer from starting.
        val supportedAbis = runCatching { Build.SUPPORTED_ABIS }.getOrNull().orEmpty()
        val abis = supportedAbis
            .filter { it.isNotBlank() }
            .joinToString(",")
            .ifBlank { "unknown" }
        return DeviceSnapshot(
            displayName = displayName,
            androidRelease = Build.VERSION.RELEASE.clean(),
            securityPatch = Build.VERSION.SECURITY_PATCH.clean(),
            primaryAbi = supportedAbis.firstOrNull().clean(),
            supportedAbis = abis,
            manufacturer = manufacturer,
            brand = Build.BRAND.clean(),
            hardware = Build.HARDWARE.clean(),
            board = Build.BOARD.clean(),
            product = Build.PRODUCT.clean(),
            rooted = RootDetector.isLikelyRooted(),
        )
    }

    private fun String?.clean(): String {
        return this?.trim()?.takeIf { it.isNotEmpty() } ?: "unknown"
    }
}
