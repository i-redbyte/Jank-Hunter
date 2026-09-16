package io.jankhunter.runtime

import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.io.BinaryLogFileHeader
import io.jankhunter.runtime.internal.io.BinaryLogWriter
import io.jankhunter.runtime.internal.io.LogGrowthManager
import io.jankhunter.runtime.internal.io.LogGrowthSessionBinding
import io.jankhunter.runtime.internal.io.LogQualityCounters
import java.io.File
import org.junit.Assert.assertTrue
import org.junit.Test

class LogGrowthTerminalArtTest {
    @Test
    fun discardedInitialGrowthSnapshotsProduceReadableTerminalLogs() {
        val files = InstrumentationRegistry.getInstrumentation().context.filesDir
        for (reason in listOf("io", "size", "budget")) {
            val directory = File(files, "growth-terminal-$reason")
            directory.deleteRecursively()
            assertTrue(directory.mkdirs())
            val file = File(directory, "terminal.jhlog")
            val header = BinaryLogFileHeader(
                runId = BinaryLogFileHeader.randomId(), processInstanceId = BinaryLogFileHeader.randomId(),
                sessionId = BinaryLogFileHeader.randomId(), segmentIndex = 0L, osPid = 1L,
                collectorStartElapsedUs = 1L, segmentStartElapsedUs = 1L, segmentStartUnixMs = 1L,
                identitySource = 0L, processName = "unknown", symbolNamespace = ByteArray(0),
            )
            val writer = BinaryLogWriter(file, 100, 1024, header, LogQualityCounters(),
                logGrowth = LogGrowthSessionBinding(LogGrowthManager(directory), "2026-09-15", 1_000_000L))
            when (reason) {
                "io" -> assertTrue(writer.sealIoError())
                "size" -> writer.sealSizeLimit()
                else -> writer.sealStorageBudget()
            }
            assertTrue(file.length() > 0L)
            file.copyTo(File(files, "growth-terminal-$reason-5.1.0.jhlog"), overwrite = true)
        }
    }
}
