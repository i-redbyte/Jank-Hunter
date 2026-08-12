package io.jankhunter.sample

import android.os.SystemClock
import androidx.test.core.app.ActivityScenario
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.runner.lifecycle.ActivityLifecycleMonitorRegistry
import androidx.test.runner.lifecycle.Stage
import io.jankhunter.sample.automatic.ScenarioStep
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterConfig
import java.io.File
import java.io.ByteArrayInputStream
import java.util.zip.GZIPInputStream
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class SampleEndToEndLogTest {
    @Test
    fun recordsCompleteAutomaticScenario() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val context = instrumentation.targetContext
        val logDir = File(context.filesDir, "jankhunter-e2e")
        val config = JankHunterConfig.fromManifest(context)
            .toBuilder()
            .flushIntervalMs(250)
            .logDirectory(logDir)
            .retainedHeapDumpDirectory(logDir)
            .build()
        instrumentation.runOnMainSync {
            JankHunter.shutdown()
            logDir.deleteRecursively()
            logDir.mkdirs()
            JankHunter.init(context, config)
        }

        try {
            ActivityScenario.launch(MainActivity::class.java).use {
                waitForResultRoute(instrumentation)
                SystemClock.sleep(config.retainedObjectDelayMs() + GC_AND_SCHEDULER_SETTLE_MS)
                val heapDump = waitForStableArtifact(logDir, "hprof", HEAP_DUMP_TIMEOUT_MS)
                assertTrue("expected non-empty .hprof at ${heapDump.absolutePath}", heapDump.length() > 0)

                instrumentation.runOnMainSync {
                    JankHunter.flush()
                }
                SystemClock.sleep(FINAL_FLUSH_DELAY_MS)
            }
        } finally {
            instrumentation.runOnMainSync {
                JankHunter.shutdown()
            }
        }
        val logFile = waitForLog(logDir)
        assertTrue("expected non-empty .jhlog at ${logFile.absolutePath}", logFile.length() > 0)
        val agentTypes = committedAgentSemanticTypes(logFile)
        assertTrue("expected ART TI status in committed .jhlog", AGENT_STATUS in agentTypes)
        assertTrue("expected ART TI capabilities in committed .jhlog", AGENT_CAPABILITY in agentTypes)
        assertTrue("expected ART TI quality in committed .jhlog", AGENT_QUALITY in agentTypes)
        assertTrue("expected ART TI clock calibration in committed .jhlog", AGENT_CLOCK_SYNC in agentTypes)
    }

    /** Reads only committed chunk structure and numeric agent type IDs; no app payload leaves the process. */
    private fun committedAgentSemanticTypes(file: File): Set<Int> {
        val input = file.readBytes()
        require(input.size >= FILE_PREFIX_BYTES)
        val headerLength = uint32Le(input, FILE_MAGIC_BYTES).toInt()
        var offset = FILE_PREFIX_BYTES + headerLength
        val result = LinkedHashSet<Int>()
        while (offset + CHUNK_HEADER_BYTES + COMMIT_TRAILER_BYTES <= input.size) {
            if (!input.regionMatches(offset, CHUNK_MAGIC)) break
            val flags = uint16Le(input, offset + 6)
            val storedLength = uint32Le(input, offset + 12).toInt()
            val rawLength = uint32Le(input, offset + 16).toInt()
            val recordCount = uint32Le(input, offset + 20).toInt()
            val payloadStart = offset + CHUNK_HEADER_BYTES
            val trailerStart = payloadStart + storedLength
            if (storedLength < 0 || rawLength < 0 || recordCount < 0 ||
                trailerStart + COMMIT_TRAILER_BYTES > input.size ||
                !input.regionMatches(trailerStart, COMMIT_MAGIC)
            ) {
                break
            }
            val stored = input.copyOfRange(payloadStart, trailerStart)
            val raw = if (flags and CHUNK_FLAG_GZIP != 0) {
                GZIPInputStream(ByteArrayInputStream(stored)).use { it.readBytes() }
            } else {
                stored
            }
            if (raw.size != rawLength) break
            scanAgentTypes(raw, recordCount, result)
            offset = trailerStart + COMMIT_TRAILER_BYTES
        }
        return result
    }

    private fun scanAgentTypes(raw: ByteArray, recordCount: Int, output: MutableSet<Int>) {
        val cursor = Cursor(raw)
        repeat(recordCount) {
            val bodyLength = cursor.uvarint().toInt()
            val bodyEnd = cursor.offset + bodyLength
            if (bodyLength < 0 || bodyEnd !in cursor.offset..raw.size) return
            val recordType = cursor.uvarint().toInt()
            val envelope = cursor.uvarint()
            if (envelope and ENVELOPE_HAS_TIME != 0L) cursor.uvarint()
            if (envelope and ENVELOPE_HAS_THREAD != 0L) cursor.uvarint()
            if (envelope and ENVELOPE_HAS_CONTEXT != 0L && envelope and ENVELOPE_SAME_CONTEXT == 0L) {
                val presence = cursor.uvarint()
                repeat(4) { bit -> if (presence and (1L shl bit) != 0L) cursor.symbolRef() }
            }
            if (envelope and ENVELOPE_HAS_ATTRIBUTES != 0L) cursor.uvarint()
            if (recordType == TYPE_AGENT_EVENT && cursor.offset < bodyEnd) {
                output += cursor.uvarint().toInt()
            }
            cursor.offset = bodyEnd
        }
    }

    private fun ByteArray.regionMatches(offset: Int, expected: ByteArray): Boolean {
        return offset >= 0 && offset + expected.size <= size && expected.indices.all { this[offset + it] == expected[it] }
    }

    private fun uint16Le(bytes: ByteArray, offset: Int): Int {
        return (bytes[offset].toInt() and 0xff) or ((bytes[offset + 1].toInt() and 0xff) shl 8)
    }

    private fun uint32Le(bytes: ByteArray, offset: Int): Long {
        var value = 0L
        repeat(4) { index -> value = value or ((bytes[offset + index].toLong() and 0xffL) shl (index * 8)) }
        return value
    }

    private class Cursor(private val bytes: ByteArray) {
        var offset: Int = 0

        fun uvarint(): Long {
            var value = 0L
            var shift = 0
            while (offset < bytes.size && shift < 64) {
                val byte = bytes[offset++].toInt() and 0xff
                value = value or ((byte and 0x7f).toLong() shl shift)
                if (byte and 0x80 == 0) return value
                shift += 7
            }
            throw IllegalArgumentException("truncated uvarint")
        }

        fun symbolRef() {
            if (uvarint() == 1L) offset = (offset + Long.SIZE_BYTES).coerceAtMost(bytes.size)
        }
    }

    private fun waitForResultRoute(instrumentation: android.app.Instrumentation) {
        val deadline = SystemClock.elapsedRealtime() + SCENARIO_TIMEOUT_MS
        while (SystemClock.elapsedRealtime() < deadline) {
            var resultVisible = false
            instrumentation.runOnMainSync {
                resultVisible = ActivityLifecycleMonitorRegistry.getInstance()
                    .getActivitiesInStage(Stage.RESUMED)
                    .any { activity -> activity is MainActivity } &&
                    JankHunter.currentScreen() == ScenarioStep.RESULT.screenName
            }
            if (resultVisible) return
            SystemClock.sleep(POLL_INTERVAL_MS)
        }
        fail("automatic scenario did not reach ${ScenarioStep.RESULT.screenName}")
    }

    private fun waitForStableArtifact(logDir: File, extension: String, timeoutMs: Long): File {
        val deadline = SystemClock.elapsedRealtime() + timeoutMs
        var lastPath = ""
        var lastSize = -1L
        var stableSamples = 0
        while (SystemClock.elapsedRealtime() < deadline) {
            val file = logDir
                .listFiles { candidate -> candidate.extension == extension && candidate.length() > 0 }
                ?.maxByOrNull { it.lastModified() }
            if (file != null) {
                if (file.absolutePath == lastPath && file.length() == lastSize) {
                    stableSamples++
                    if (stableSamples >= REQUIRED_STABLE_SAMPLES) return file
                } else {
                    lastPath = file.absolutePath
                    lastSize = file.length()
                    stableSamples = 0
                }
            }
            SystemClock.sleep(POLL_INTERVAL_MS)
        }
        fail("no stable .$extension created in ${logDir.absolutePath}")
        throw AssertionError("unreachable")
    }

    private fun waitForLog(logDir: File): File {
        val deadline = SystemClock.elapsedRealtime() + 5_000
        while (SystemClock.elapsedRealtime() < deadline) {
            val file = logDir
                .listFiles { candidate -> candidate.extension == "jhlog" && candidate.length() > 0 }
                ?.maxByOrNull { it.lastModified() }
            if (file != null) return file
            SystemClock.sleep(100)
        }
        fail("no .jhlog created in ${logDir.absolutePath}")
        throw AssertionError("unreachable")
    }

    private companion object {
        const val SCENARIO_TIMEOUT_MS = 70_000L
        const val HEAP_DUMP_TIMEOUT_MS = 45_000L
        const val GC_AND_SCHEDULER_SETTLE_MS = 6_000L
        const val FINAL_FLUSH_DELAY_MS = 1_000L
        const val POLL_INTERVAL_MS = 250L
        const val REQUIRED_STABLE_SAMPLES = 3
        const val FILE_MAGIC_BYTES = 8
        const val FILE_PREFIX_BYTES = 16
        const val CHUNK_HEADER_BYTES = 32
        const val COMMIT_TRAILER_BYTES = 20
        const val CHUNK_FLAG_GZIP = 1
        const val TYPE_AGENT_EVENT = 17
        const val AGENT_STATUS = 1
        const val AGENT_CAPABILITY = 2
        const val AGENT_QUALITY = 3
        const val AGENT_CLOCK_SYNC = 10
        const val ENVELOPE_HAS_TIME = 1L
        const val ENVELOPE_HAS_THREAD = 1L shl 1
        const val ENVELOPE_HAS_CONTEXT = 1L shl 2
        const val ENVELOPE_SAME_CONTEXT = 1L shl 3
        const val ENVELOPE_HAS_ATTRIBUTES = 1L shl 4
        val CHUNK_MAGIC = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'C'.code.toByte(), '9'.code.toByte())
        val COMMIT_MAGIC = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'C'.code.toByte(), 'M'.code.toByte())
    }
}
