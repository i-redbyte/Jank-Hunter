package io.jankhunter.runtime.internal.system

import android.app.ActivityManager
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.net.TrafficStats
import android.os.BatteryManager
import android.os.Build
import android.os.PowerManager
import android.os.Process
import android.os.StatFs
import io.jankhunter.runtime.JankHunter
import java.io.File
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.math.max

internal class SystemContextSampler(
    context: Context,
    private val intervalMs: Long,
) {
    private val appContext = context.applicationContext
    private val activityManager = appContext.getSystemService(Context.ACTIVITY_SERVICE) as? ActivityManager
    private val connectivityManager = appContext.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager
    private val powerManager = appContext.getSystemService(Context.POWER_SERVICE) as? PowerManager
    private val cpuSampler = ProcCpuSampler(
        readProcessStat = { readTextFile(PROC_SELF_STAT) },
        readSystemStat = { readFirstLine(PROC_STAT) },
    )
    private val running = AtomicBoolean(false)
    private var thread: Thread? = null

    fun start() {
        if (!running.compareAndSet(false, true)) return
        thread = Thread({ loop() }, "JankHunterSystemSampler").apply {
            isDaemon = true
            start()
        }
    }

    fun stop() {
        running.set(false)
        thread?.interrupt()
    }

    private fun loop() {
        while (running.get()) {
            sampleOnce()
            try {
                Thread.sleep(max(1_000L, intervalMs))
            } catch (_: InterruptedException) {
                Thread.currentThread().interrupt()
            }
        }
    }

    private fun sampleOnce() {
        val memory = readMemory()
        val battery = readBattery()
        val network = readNetwork()
        val traffic = readTraffic()
        val storage = readStorage()
        val cpu = cpuSampler.sample()

        JankHunter.recordContext(
            network.kind,
            battery.percent,
            memory.availKb,
            battery.state,
            battery.temperatureDeciC,
            memory.lowMemory,
            network.metered,
            network.validated,
            traffic.rxBytes,
            traffic.txBytes,
            memory.totalKb,
            storage.freeKb,
            storage.totalKb,
            network.vpn,
        )
        recordBatteryPowerMetrics(battery)
        recordCpuMetrics(cpu)
    }

    private fun readMemory(): MemorySnapshot {
        val info = ActivityManager.MemoryInfo()
        return try {
            activityManager?.getMemoryInfo(info)
            MemorySnapshot(
                availKb = max(0L, info.availMem / 1024L),
                totalKb = max(0L, info.totalMem / 1024L),
                lowMemory = info.lowMemory,
            )
        } catch (_: Exception) {
            MemorySnapshot(0, 0, false)
        }
    }

    private fun readBattery(): BatterySnapshot {
        val intent = appContext.registerReceiver(null, IntentFilter(Intent.ACTION_BATTERY_CHANGED))
        val level = intent?.getIntExtra(BatteryManager.EXTRA_LEVEL, -1) ?: -1
        val scale = intent?.getIntExtra(BatteryManager.EXTRA_SCALE, -1) ?: -1
        val percent = if (level >= 0 && scale > 0) (level * 100) / scale else 0
        return BatterySnapshot(
            percent = percent.coerceIn(0, 100),
            state = intent?.getIntExtra(BatteryManager.EXTRA_STATUS, BatteryManager.BATTERY_STATUS_UNKNOWN)
                ?: BatteryManager.BATTERY_STATUS_UNKNOWN,
            temperatureDeciC = intent?.getIntExtra(BatteryManager.EXTRA_TEMPERATURE, 0) ?: 0,
            plugged = intent?.getIntExtra(BatteryManager.EXTRA_PLUGGED, 0) ?: 0,
            voltageMv = intent?.getIntExtra(BatteryManager.EXTRA_VOLTAGE, 0) ?: 0,
            health = intent?.getIntExtra(BatteryManager.EXTRA_HEALTH, BatteryManager.BATTERY_HEALTH_UNKNOWN)
                ?: BatteryManager.BATTERY_HEALTH_UNKNOWN,
        )
    }

    private fun readNetwork(): NetworkSnapshot {
        return try {
            val network = connectivityManager?.activeNetwork
                ?: return NetworkSnapshot(NETWORK_OFFLINE, metered = false, validated = false, vpn = false)
            val capabilities = connectivityManager.getNetworkCapabilities(network)
            val vpn = capabilities?.hasTransport(NetworkCapabilities.TRANSPORT_VPN) == true
            val kind = when {
                capabilities == null -> NETWORK_UNKNOWN
                capabilities.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> NETWORK_WIFI
                capabilities.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> NETWORK_CELLULAR
                capabilities.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) -> NETWORK_ETHERNET
                vpn -> NETWORK_VPN
                else -> NETWORK_UNKNOWN
            }
            NetworkSnapshot(
                kind = kind,
                metered = connectivityManager.isActiveNetworkMetered,
                validated = capabilities?.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED) == true,
                vpn = vpn,
            )
        } catch (_: SecurityException) {
            NetworkSnapshot(NETWORK_UNKNOWN, metered = false, validated = false, vpn = false)
        } catch (_: Exception) {
            NetworkSnapshot(NETWORK_UNKNOWN, metered = false, validated = false, vpn = false)
        }
    }

    private fun readTraffic(): TrafficSnapshot {
        val uid = Process.myUid()
        return TrafficSnapshot(
            rxBytes = sanitizeTraffic(TrafficStats.getUidRxBytes(uid)),
            txBytes = sanitizeTraffic(TrafficStats.getUidTxBytes(uid)),
        )
    }

    private fun sanitizeTraffic(value: Long): Long {
        return if (value == TrafficStats.UNSUPPORTED.toLong() || value < 0L) 0L else value
    }

    private fun readStorage(): StorageSnapshot {
        return try {
            val statFs = StatFs(appContext.filesDir.absolutePath)
            StorageSnapshot(
                freeKb = max(0L, statFs.availableBytes / 1024L),
                totalKb = max(0L, statFs.totalBytes / 1024L),
            )
        } catch (_: Exception) {
            StorageSnapshot(0, 0)
        }
    }

    private fun recordBatteryPowerMetrics(battery: BatterySnapshot) {
        JankHunter.recordGauge("battery.level_pct", battery.percent.toLong())
        if (battery.temperatureDeciC >= 0) {
            JankHunter.recordGauge("battery.temperature_deci_c", battery.temperatureDeciC.toLong())
        } else {
            JankHunter.recordCounter("battery.temperature.negative.count", 1)
        }
        JankHunter.recordGauge("battery.status", battery.state.toLong())
        JankHunter.recordGauge("battery.plugged", battery.plugged.toLong())
        JankHunter.recordGauge("battery.voltage_mv", battery.voltageMv.toLong())
        JankHunter.recordGauge("battery.health", battery.health.toLong())
        val charging = battery.state == BatteryManager.BATTERY_STATUS_CHARGING ||
            battery.state == BatteryManager.BATTERY_STATUS_FULL
        JankHunter.recordGauge("battery.charging", if (charging) 1L else 0L)

        val power = powerManager ?: return
        JankHunter.recordGauge("device.power_save_mode", if (power.isPowerSaveMode) 1L else 0L)
        JankHunter.recordGauge("device.interactive", if (power.isInteractive) 1L else 0L)
        JankHunter.recordGauge("device.idle_mode", if (power.isDeviceIdleMode) 1L else 0L)
        if (Build.VERSION.SDK_INT >= 29) {
            JankHunter.recordGauge("device.thermal.status", power.currentThermalStatus.toLong())
        }
    }

    private fun recordCpuMetrics(cpu: ProcCpuSampler.CpuSample?) {
        if (cpu == null) return
        JankHunter.recordGauge("process.cpu.device_percent_x100", cpu.processDevicePercentX100)
        JankHunter.recordGauge("process.cpu.core_percent_x100", cpu.processCorePercentX100)
        JankHunter.recordGauge("device.cpu.busy_percent_x100", cpu.deviceBusyPercentX100)
        JankHunter.recordGauge("device.cpu.core_count", cpu.coreCount.toLong())
    }

    private fun readTextFile(path: String): String? {
        return try {
            File(path).readText()
        } catch (_: Exception) {
            null
        }
    }

    private fun readFirstLine(path: String): String? {
        return try {
            File(path).bufferedReader().use { it.readLine() }
        } catch (_: Exception) {
            null
        }
    }

    private data class MemorySnapshot(
        val availKb: Long,
        val totalKb: Long,
        val lowMemory: Boolean,
    )

    private data class BatterySnapshot(
        val percent: Int,
        val state: Int,
        val temperatureDeciC: Int,
        val plugged: Int,
        val voltageMv: Int,
        val health: Int,
    )

    private data class NetworkSnapshot(
        val kind: Int,
        val metered: Boolean,
        val validated: Boolean,
        val vpn: Boolean,
    )

    private data class TrafficSnapshot(
        val rxBytes: Long,
        val txBytes: Long,
    )

    private data class StorageSnapshot(
        val freeKb: Long,
        val totalKb: Long,
    )

    companion object {
        const val NETWORK_UNKNOWN = 0
        const val NETWORK_OFFLINE = 1
        const val NETWORK_WIFI = 2
        const val NETWORK_CELLULAR = 3
        const val NETWORK_ETHERNET = 4
        const val NETWORK_VPN = 5
        private const val PROC_SELF_STAT = "/proc/self/stat"
        private const val PROC_STAT = "/proc/stat"
    }
}
