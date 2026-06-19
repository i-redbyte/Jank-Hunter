package io.jankhunter.gradle

import java.util.Locale
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReference
import kotlin.math.ceil

private const val MIN_UPDATE_INTERVAL_NANOS = 250_000_000L
private const val UPDATE_EVERY_CLASSES = 25

internal object AsmProgressReporter {
    private val trackers = ConcurrentHashMap<String, AsmProgressTracker>()
    private val outputLock = Any()
    private var lastLineLength = 0

    fun recordScanned(label: String, className: String, matched: Boolean) {
        tracker(label).recordScanned(className, matched)
    }

    fun recordInstrumented(label: String, className: String, hooks: String) {
        tracker(label).recordInstrumented(className, hooks)
    }

    private fun tracker(label: String): AsmProgressTracker {
        return trackers.computeIfAbsent(label) { key ->
            AsmProgressTracker(key, ::printLine)
        }
    }

    private fun printLine(line: String) {
        synchronized(outputLock) {
            val padded = if (line.length < lastLineLength) {
                line.padEnd(lastLineLength)
            } else {
                line
            }
            lastLineLength = line.length
            System.out.print("\r$padded")
            System.out.flush()
        }
    }
}

internal class AsmProgressTracker(
    private val label: String,
    private val printer: (String) -> Unit,
) {
    private val startedAtNanos = System.nanoTime()
    private val scanned = AtomicInteger()
    private val matched = AtomicInteger()
    private val instrumented = AtomicInteger()
    private val lastPrintNanos = AtomicLong(0L)
    private val latestClass = AtomicReference("")
    private val latestHooks = AtomicReference("")

    fun recordScanned(className: String, isMatched: Boolean) {
        scanned.incrementAndGet()
        if (isMatched) {
            matched.incrementAndGet()
            latestClass.compareAndSet("", className)
        }
    }

    fun recordInstrumented(className: String, hooks: String) {
        val done = instrumented.incrementAndGet()
        latestClass.set(className)
        latestHooks.set(hooks)

        val now = System.nanoTime()
        if (shouldPrint(done, now)) {
            printer(formatAsmProgressLine(snapshot(now)))
        }
    }

    private fun shouldPrint(done: Int, now: Long): Boolean {
        if (done <= 3 || done % UPDATE_EVERY_CLASSES == 0) {
            lastPrintNanos.set(now)
            return true
        }

        val last = lastPrintNanos.get()
        return now - last >= MIN_UPDATE_INTERVAL_NANOS &&
            lastPrintNanos.compareAndSet(last, now)
    }

    private fun snapshot(now: Long): AsmProgressSnapshot {
        val elapsedNanos = (now - startedAtNanos).coerceAtLeast(1L)
        val done = instrumented.get()
        val found = matched.get()
        val queue = (found - done).coerceAtLeast(0)
        val rate = done * 1_000_000_000.0 / elapsedNanos.toDouble()
        val etaSeconds = if (queue > 0 && rate > 0.0) queue / rate else 0.0

        return AsmProgressSnapshot(
            label = label,
            scanned = scanned.get(),
            matched = found,
            instrumented = done,
            queued = queue,
            ratePerSecond = rate,
            etaSeconds = etaSeconds,
            latestClass = latestClass.get(),
            hooks = latestHooks.get(),
        )
    }
}

internal data class AsmProgressSnapshot(
    val label: String,
    val scanned: Int,
    val matched: Int,
    val instrumented: Int,
    val queued: Int,
    val ratePerSecond: Double,
    val etaSeconds: Double,
    val latestClass: String,
    val hooks: String,
)

internal fun formatAsmProgressLine(snapshot: AsmProgressSnapshot): String {
    val rate = String.format(Locale.US, "%.1f", snapshot.ratePerSecond)
    return "Jank Hunter ASM [${snapshot.label}] " +
        "готово=${snapshot.instrumented}/${snapshot.matched} " +
        "скан=${snapshot.scanned} " +
        "очередь=${snapshot.queued} " +
        "скорость=${rate}кл/с " +
        "ETA~${formatEta(snapshot.etaSeconds)} " +
        "класс=${compactClassName(snapshot.latestClass)} " +
        "hooks=${snapshot.hooks}"
}

private fun formatEta(seconds: Double): String {
    if (seconds <= 0.0) return "0с"
    if (seconds < 1.0) return "<1с"

    val roundedSeconds = ceil(seconds).toLong()
    return when {
        roundedSeconds < 60L -> "${roundedSeconds}с"
        roundedSeconds < 3_600L -> {
            val minutes = roundedSeconds / 60L
            val restSeconds = roundedSeconds % 60L
            "${minutes}м${restSeconds.toString().padStart(2, '0')}с"
        }

        else -> {
            val hours = roundedSeconds / 3_600L
            val minutes = (roundedSeconds % 3_600L) / 60L
            "${hours}ч${minutes.toString().padStart(2, '0')}м"
        }
    }
}

private fun compactClassName(className: String): String {
    val normalized = className.replace('/', '.')
    return if (normalized.length <= 96) {
        normalized
    } else {
        "...${normalized.takeLast(93)}"
    }
}
