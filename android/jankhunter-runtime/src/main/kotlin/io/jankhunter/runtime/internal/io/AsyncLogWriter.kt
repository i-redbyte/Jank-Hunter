package io.jankhunter.runtime.internal.io

import android.os.SystemClock
import io.jankhunter.runtime.JankHunterConfig
import java.io.File
import java.io.IOException
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong

class AsyncLogWriter private constructor(
    private val directory: File,
    private val config: JankHunterConfig,
    private val processFileSuffix: String,
) {
    private val queue = ArrayBlockingQueue<Action>(config.maxQueueSize())
    private val accepting = AtomicBoolean(true)
    private val running = AtomicBoolean(true)
    private val writerClosed = AtomicBoolean(false)
    private val dropped = AtomicLong()
    private val pendingIoErrors = AtomicLong()
    private val pendingIoLostEvents = AtomicLong()
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
        if (value < 0) {
            offer { it.counter("jankhunter.metric.invalid_negative.counter.count", 1) }
            return
        }
        offer { it.counter(name, value) }
    }

    fun gauge(name: String?, value: Long) {
        if (value < 0) {
            offer { it.counter("jankhunter.metric.invalid_negative.gauge.count", 1) }
            return
        }
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
        offer {
            flushIfNeeded(force = true)
        }
    }

    fun flushBlocking(timeoutMs: Long = maxOf(1000L, config.flushIntervalMs() + 500L)): Boolean {
        if (!accepting.get()) return false
        val latch = CountDownLatch(1)
        val timeout = timeoutMs.coerceAtLeast(1L)
        val accepted = offerBlocking(timeout) {
            try {
                flushIfNeeded(force = true)
            } finally {
                latch.countDown()
            }
        }
        if (!accepted) return false
        return try {
            latch.await(timeout, TimeUnit.MILLISECONDS)
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
            false
        }
    }

    fun close(timeoutMs: Long = closeTimeoutMs()): Boolean {
        accepting.set(false)
        running.set(false)
        val finished = waitForWorker(timeoutMs.coerceAtLeast(1L))
        if (finished) {
            closeWriterOnce()
        }
        return finished
    }

    private fun offer(action: Action) {
        if (!accepting.get()) return
        if (!queue.offer(action)) {
            dropped.incrementAndGet()
        }
    }

    private fun offerBlocking(timeoutMs: Long, action: Action): Boolean {
        if (!accepting.get()) return false
        return offerControlBlocking(timeoutMs, action)
    }

    private fun offerControlBlocking(timeoutMs: Long, action: Action): Boolean {
        return try {
            if (queue.offer(action, timeoutMs, TimeUnit.MILLISECONDS)) {
                true
            } else {
                dropped.incrementAndGet()
                false
            }
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
            false
        }
    }

    private fun closeTimeoutMs(): Long {
        return maxOf(1000L, config.flushIntervalMs() + 500L)
    }

    private fun waitForWorker(timeoutMs: Long): Boolean {
        val deadlineNs = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(timeoutMs)
        var interrupted = false
        while (worker.isAlive) {
            val remainingNs = deadlineNs - System.nanoTime()
            if (remainingNs <= 0L) break
            try {
                val remainingMs = TimeUnit.NANOSECONDS.toMillis(remainingNs).coerceAtLeast(1L)
                worker.join(minOf(CLOSE_JOIN_POLL_MS, remainingMs))
            } catch (_: InterruptedException) {
                interrupted = true
                break
            }
        }
        if (interrupted) {
            Thread.currentThread().interrupt()
        }
        return !worker.isAlive
    }

    private fun loop() {
        try {
            while (running.get() || queue.isNotEmpty()) {
                try {
                    queue.poll(250, TimeUnit.MILLISECONDS)?.let(::writeAction)
                    writeDroppedIfNeeded()
                    flushIfNeeded(force = false)
                } catch (_: InterruptedException) {
                    if (!running.get() && queue.isEmpty()) {
                        break
                    }
                } catch (_: IOException) {
                    pendingIoErrors.incrementAndGet()
                    recoverWriterAfterIoError()
                }
            }
            try {
                writeDroppedIfNeeded()
                flushIfNeeded(force = true)
            } catch (_: IOException) {
                pendingIoErrors.incrementAndGet()
                recoverWriterAfterIoError()
                flushIfNeeded(force = true)
            }
        } finally {
            if (!running.get()) {
                closeWriterOnce()
            }
        }
    }

    private fun closeWriterOnce() {
        if (!writerClosed.compareAndSet(false, true)) return
        try {
            writer.close()
        } catch (_: IOException) {
        }
    }

    private fun writeDroppedIfNeeded() {
        val lost = dropped.getAndSet(0)
        if (lost <= 0) return
        writer.counter("jankhunter.events_dropped.count", lost)
        writePendingIoErrors()
        rotateIfNeeded()
    }

    private fun writeAction(action: Action) {
        try {
            action.write(writer)
        } catch (_: IOException) {
            pendingIoErrors.incrementAndGet()
            recoverWriterAfterIoError()
            try {
                action.write(writer)
            } catch (_: IOException) {
                pendingIoErrors.incrementAndGet()
                pendingIoLostEvents.incrementAndGet()
                recoverWriterAfterIoError()
                return
            }
        }
        writePendingIoErrors()
        rotateIfNeeded()
    }

    private fun rotateIfNeeded() {
        val maxLogBytes = config.maxLogBytes()
        if (maxLogBytes <= 0 || writer.bytesWritten() < maxLogBytes) return

        writer.close()
        writer = openSegment()
        lastFlushAtMs = SystemClock.elapsedRealtime()
        bootstrap?.write(writer)
        writePendingIoErrors()
        enforceRetention()
    }

    private fun flushIfNeeded(force: Boolean) {
        val now = SystemClock.elapsedRealtime()
        val interval = config.flushIntervalMs()
        if (!force && (interval <= 0 || now - lastFlushAtMs < interval)) return

        try {
            writePendingIoErrors()
            writer.flush()
            lastFlushAtMs = now
        } catch (_: IOException) {
            pendingIoErrors.incrementAndGet()
            recoverWriterAfterIoError()
        }
    }

    private fun recoverWriterAfterIoError() {
        try {
            writer.close()
        } catch (_: IOException) {
        }
        try {
            writer = openSegment()
            lastFlushAtMs = SystemClock.elapsedRealtime()
            bootstrap?.write(writer)
            writePendingIoErrors()
            enforceRetention()
        } catch (_: IOException) {
            pendingIoErrors.incrementAndGet()
        }
    }

    private fun writePendingIoErrors() {
        val errors = pendingIoErrors.getAndSet(0)
        val lostEvents = pendingIoLostEvents.getAndSet(0)
        if (errors <= 0 && lostEvents <= 0) return
        try {
            if (errors > 0) {
                writer.counter("jankhunter.writer_io_error.count", errors)
            }
            if (lostEvents > 0) {
                writer.counter("jankhunter.writer_event_lost_on_io.count", lostEvents)
            }
        } catch (error: IOException) {
            if (errors > 0) {
                pendingIoErrors.addAndGet(errors)
            }
            if (lostEvents > 0) {
                pendingIoLostEvents.addAndGet(lostEvents)
            }
            throw error
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

    internal fun enqueueForTest(action: Action) {
        offer(action)
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
        private const val CLOSE_JOIN_POLL_MS = 250L

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
