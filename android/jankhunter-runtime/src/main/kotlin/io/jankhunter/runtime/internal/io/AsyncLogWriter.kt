package io.jankhunter.runtime.internal.io

import android.os.SystemClock
import io.jankhunter.runtime.JankHunterConfig
import java.io.File
import java.io.IOException
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong

class AsyncLogWriter private constructor(
    private val directory: File,
    private val config: JankHunterConfig,
    private val processFileSuffix: String,
) {
    private val queue = ArrayBlockingQueue<Action>(config.maxQueueSize())
    private val running = AtomicBoolean(true)
    private val dropped = AtomicLong()
    private val filePrefix = "session-$processFileSuffix-"
    private var nextSegmentId = 1L
    private var writer = openSegment()
    private var lastFlushAtMs = SystemClock.elapsedRealtime()
    @Volatile
    private var bootstrap: SessionBootstrap? = null
    private val worker = Thread({ loop() }, "JankHunterWriter").apply {
        isDaemon = true
        start()
    }

    init {
        enforceRetention()
    }

    fun session(
        appVersion: String?,
        build: String?,
        device: String?,
        sdkInt: Int,
        processName: String?,
        androidRelease: String?,
        securityPatch: String?,
        primaryAbi: String?,
        supportedAbis: String?,
        manufacturer: String?,
        brand: String?,
        hardware: String?,
        board: String?,
        product: String?,
        deviceRooted: Boolean,
    ) {
        bootstrap = SessionBootstrap(
            appVersion,
            build,
            device,
            sdkInt,
            processName,
            androidRelease,
            securityPatch,
            primaryAbi,
            supportedAbis,
            manufacturer,
            brand,
            hardware,
            board,
            product,
            deviceRooted,
        )
        offer {
            it.session(
                appVersion,
                build,
                device,
                sdkInt,
                processName,
                androidRelease,
                securityPatch,
                primaryAbi,
                supportedAbis,
                manufacturer,
                brand,
                hardware,
                board,
                product,
                deviceRooted,
            )
        }
    }

    fun screen(screen: String?) {
        offer { it.screen(screen) }
    }

    fun context(
        networkKind: Int,
        batteryPct: Int,
        availMemoryKb: Long,
        batteryState: Int,
        batteryTempDeciC: Int,
        lowMemory: Boolean,
        networkMetered: Boolean,
        networkValidated: Boolean,
        rxBytes: Long,
        txBytes: Long,
        totalMemoryKb: Long,
        freeStorageKb: Long,
        totalStorageKb: Long,
        networkVpn: Boolean,
    ) {
        offer {
            it.context(
                networkKind,
                batteryPct,
                availMemoryKb,
                batteryState,
                batteryTempDeciC,
                lowMemory,
                networkMetered,
                networkValidated,
                rxBytes,
                txBytes,
                totalMemoryKb,
                freeStorageKb,
                totalStorageKb,
                networkVpn,
            )
        }
    }

    fun http(
        owner: String?,
        route: String?,
        durationMs: Long,
        dnsMs: Long,
        connectMs: Long,
        ttfbMs: Long,
        statusClass: Int,
        rxBytes: Long,
        txBytes: Long,
        flags: Long,
    ) {
        offer { it.http(owner, route, durationMs, dnsMs, connectMs, ttfbMs, statusClass, rxBytes, txBytes, flags) }
    }

    fun stall(owner: String?, stackHint: String?, durationMs: Long) {
        offer { it.stall(owner, stackHint, durationMs) }
    }

    fun memory(pssKb: Long, javaHeapKb: Long, nativeHeapKb: Long) {
        offer { it.memory(pssKb, javaHeapKb, nativeHeapKb) }
    }

    fun retained(
        screen: String?,
        owner: String?,
        flow: String?,
        step: String?,
        className: String?,
        holder: String?,
        ageMs: Long,
        count: Long,
    ) {
        offer { it.retained(screen, owner, flow, step, className, holder, ageMs, count) }
    }

    fun uiWindow(
        screen: String?,
        windowMs: Long,
        frameCount: Long,
        jankCount: Long,
        p50Ms: Long,
        p95Ms: Long,
        p99Ms: Long,
    ) {
        offer { it.uiWindow(screen, windowMs, frameCount, jankCount, p50Ms, p95Ms, p99Ms) }
    }

    fun counter(name: String?, value: Long) {
        offer { it.counter(name, value) }
    }

    fun gauge(name: String?, value: Long) {
        offer { it.gauge(name, value) }
    }

    fun flowContext(screen: String?, owner: String?, flow: String?, step: String?) {
        offer { it.flowContext(screen, owner, flow, step) }
    }

    fun logSpam(
        screen: String?,
        owner: String?,
        flow: String?,
        step: String?,
        source: String?,
        level: Int,
        count: Long,
    ) {
        offer { it.logSpam(screen, owner, flow, step, source, level, count) }
    }

    fun problemWindow(
        screen: String?,
        owner: String?,
        flow: String?,
        step: String?,
        kind: String?,
        windowMs: Long,
        count: Long,
        maxMs: Long,
    ) {
        offer { it.problemWindow(screen, owner, flow, step, kind, windowMs, count, maxMs) }
    }

    fun runtimeCall(
        screen: String?,
        caller: String?,
        flow: String?,
        step: String?,
        callee: String?,
        count: Long,
        totalMs: Long,
        maxMs: Long,
    ) {
        offer { it.runtimeCall(screen, caller, flow, step, callee, count, totalMs, maxMs) }
    }

    fun flush() {
        offer { it.flush() }
    }

    fun close() {
        running.set(false)
        try {
            worker.join(maxOf(1000L, config.flushIntervalMs() + 500L))
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
        }
        if (worker.isAlive) {
            worker.interrupt()
            try {
                worker.join(500)
            } catch (_: InterruptedException) {
                Thread.currentThread().interrupt()
            }
        }
        try {
            writer.close()
        } catch (_: IOException) {
        }
    }

    private fun offer(action: Action) {
        if (!running.get()) return
        if (!queue.offer(action)) {
            dropped.incrementAndGet()
        }
    }

    private fun loop() {
        while (running.get() || queue.isNotEmpty()) {
            try {
                queue.poll(250, TimeUnit.MILLISECONDS)?.let {
                    it.write(writer)
                    rotateIfNeeded()
                }
                val lost = dropped.getAndSet(0)
                if (lost > 0) {
                    writer.counter("jankhunter.events_dropped.count", lost)
                    rotateIfNeeded()
                }
                flushIfNeeded(force = false)
            } catch (_: InterruptedException) {
                if (!running.get() && queue.isEmpty()) {
                    break
                }
            } catch (_: IOException) {
            }
        }
        flushIfNeeded(force = true)
    }

    private fun rotateIfNeeded() {
        val maxLogBytes = config.maxLogBytes()
        if (maxLogBytes <= 0 || writer.bytesWritten() < maxLogBytes) return

        writer.close()
        writer = openSegment()
        lastFlushAtMs = SystemClock.elapsedRealtime()
        bootstrap?.write(writer)
        enforceRetention()
    }

    private fun flushIfNeeded(force: Boolean) {
        val now = SystemClock.elapsedRealtime()
        val interval = config.flushIntervalMs()
        if (!force && (interval <= 0 || now - lastFlushAtMs < interval)) return

        try {
            writer.flush()
            lastFlushAtMs = now
        } catch (_: IOException) {
        }
    }

    private fun openSegment(): BinaryLogWriter {
        val file = File(directory, "$filePrefix${System.currentTimeMillis()}-${nextSegmentId++}.jhlog")
        return BinaryLogWriter(
            file,
            config.maxDictionaryEntries(),
            config.maxDictionaryValueBytes(),
            config.logCompressionEnabled(),
        )
    }

    private fun enforceRetention() {
        LogRetention.enforce(directory, filePrefix, writer.file, config.maxLogDirectoryBytes())
    }

    private data class SessionBootstrap(
        val appVersion: String?,
        val build: String?,
        val device: String?,
        val sdkInt: Int,
        val processName: String?,
        val androidRelease: String?,
        val securityPatch: String?,
        val primaryAbi: String?,
        val supportedAbis: String?,
        val manufacturer: String?,
        val brand: String?,
        val hardware: String?,
        val board: String?,
        val product: String?,
        val deviceRooted: Boolean,
    ) {
        fun write(writer: BinaryLogWriter) {
            writer.session(
                appVersion,
                build,
                device,
                sdkInt,
                processName,
                androidRelease,
                securityPatch,
                primaryAbi,
                supportedAbis,
                manufacturer,
                brand,
                hardware,
                board,
                product,
                deviceRooted,
            )
        }
    }

    fun interface Action {
        @Throws(IOException::class)
        fun write(writer: BinaryLogWriter)
    }

    companion object {
        fun open(directory: File, config: JankHunterConfig, processFileSuffix: String): AsyncLogWriter {
            if (!directory.exists() && !directory.mkdirs()) {
                error("Cannot create Jank Hunter log directory: $directory")
            }
            return try {
                AsyncLogWriter(directory, config, processFileSuffix)
            } catch (e: IOException) {
                throw IllegalStateException("Cannot open Jank Hunter log file", e)
            }
        }
    }
}

internal object LogRetention {
    fun enforce(directory: File, filePrefix: String, currentFile: File, maxDirectoryBytes: Long) {
        if (maxDirectoryBytes <= 0) return

        val currentPath = currentFile.absolutePath
        val files = directory
            .listFiles { file ->
                file.isFile &&
                    file.name.startsWith(filePrefix) &&
                    file.name.endsWith(".jhlog")
            }
            ?.toList()
            ?: return

        var totalBytes = files.sumOf { it.length() }
        if (totalBytes <= maxDirectoryBytes) return

        files
            .filterNot { it.absolutePath == currentPath }
            .sortedWith(compareBy<File> { it.lastModified() }.thenBy { it.name })
            .forEach { file ->
                if (totalBytes <= maxDirectoryBytes) return
                val fileBytes = file.length()
                if (file.delete()) {
                    totalBytes -= fileBytes
                }
            }
    }
}
