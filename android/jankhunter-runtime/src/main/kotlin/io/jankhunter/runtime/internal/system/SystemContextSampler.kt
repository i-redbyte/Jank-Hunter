package io.jankhunter.runtime.internal.system

import android.app.ActivityManager
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.net.TrafficStats
import android.os.BatteryManager
import android.os.Process
import io.jankhunter.runtime.JankHunter
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.math.max

class SystemContextSampler(
    context: Context,
    private val intervalMs: Long,
) {
    private val appContext = context.applicationContext
    private val activityManager = appContext.getSystemService(Context.ACTIVITY_SERVICE) as? ActivityManager
    private val connectivityManager = appContext.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager
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
        )
    }

    private fun readMemory(): MemorySnapshot {
        val info = ActivityManager.MemoryInfo()
        return try {
            activityManager?.getMemoryInfo(info)
            MemorySnapshot(
                availKb = max(0L, info.availMem / 1024L),
                lowMemory = info.lowMemory,
            )
        } catch (_: Exception) {
            MemorySnapshot(0, false)
        }
    }

    private fun readBattery(): BatterySnapshot {
        val intent = appContext.registerReceiver(null, IntentFilter(Intent.ACTION_BATTERY_CHANGED))
        val level = intent?.getIntExtra(BatteryManager.EXTRA_LEVEL, -1) ?: -1
        val scale = intent?.getIntExtra(BatteryManager.EXTRA_SCALE, -1) ?: -1
        val percent = if (level >= 0 && scale > 0) (level * 100) / scale else 0
        return BatterySnapshot(
            percent = percent,
            state = intent?.getIntExtra(BatteryManager.EXTRA_STATUS, BatteryManager.BATTERY_STATUS_UNKNOWN)
                ?: BatteryManager.BATTERY_STATUS_UNKNOWN,
            temperatureDeciC = intent?.getIntExtra(BatteryManager.EXTRA_TEMPERATURE, 0) ?: 0,
        )
    }

    private fun readNetwork(): NetworkSnapshot {
        return try {
            val network = connectivityManager?.activeNetwork
                ?: return NetworkSnapshot(NETWORK_OFFLINE, metered = false, validated = false)
            val capabilities = connectivityManager.getNetworkCapabilities(network)
            val kind = when {
                capabilities == null -> NETWORK_UNKNOWN
                capabilities.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> NETWORK_WIFI
                capabilities.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> NETWORK_CELLULAR
                capabilities.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) -> NETWORK_ETHERNET
                capabilities.hasTransport(NetworkCapabilities.TRANSPORT_VPN) -> NETWORK_VPN
                else -> NETWORK_UNKNOWN
            }
            NetworkSnapshot(
                kind = kind,
                metered = connectivityManager.isActiveNetworkMetered,
                validated = capabilities?.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED) == true,
            )
        } catch (_: SecurityException) {
            NetworkSnapshot(NETWORK_UNKNOWN, metered = false, validated = false)
        } catch (_: Exception) {
            NetworkSnapshot(NETWORK_UNKNOWN, metered = false, validated = false)
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

    private data class MemorySnapshot(
        val availKb: Long,
        val lowMemory: Boolean,
    )

    private data class BatterySnapshot(
        val percent: Int,
        val state: Int,
        val temperatureDeciC: Int,
    )

    private data class NetworkSnapshot(
        val kind: Int,
        val metered: Boolean,
        val validated: Boolean,
    )

    private data class TrafficSnapshot(
        val rxBytes: Long,
        val txBytes: Long,
    )

    companion object {
        const val NETWORK_UNKNOWN = 0
        const val NETWORK_OFFLINE = 1
        const val NETWORK_WIFI = 2
        const val NETWORK_CELLULAR = 3
        const val NETWORK_ETHERNET = 4
        const val NETWORK_VPN = 5
    }
}
