package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterConfig
import io.jankhunter.runtime.JankHunterStoragePolicy
import java.io.File
import java.nio.file.Files
import java.util.UUID
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class SessionStorageWriterTest {
    @Test
    fun physicalCapSealsOneFileAndReportsSizeLimit() = assertPhysicalCap(setOf("hprof"))

    @Test
    fun artifactExemptionNeverExemptsJhlogWriter() = assertPhysicalCap(setOf("hprof", "jhlog"))

    private fun assertPhysicalCap(exemptions: Set<String>) {
        val root = Files.createTempDirectory("jh-policy-file-cap").toFile()
        val recording = ProcessRecordingSession()
        val terminal = CountDownLatch(1)
        val reason = AtomicInteger()
        val writer = AsyncLogWriterFactory(recording).open(
            root, config(root, 12 * 1024, exemptions), "main",
            onTerminalStop = { _, value, _ -> reason.set(value); terminal.countDown() },
        )
        try {
            repeat(2_000) { writer.counter("counter.${UUID.randomUUID()}", it.toLong()) }
            assertTrue(writer.close())
            assertTrue(terminal.await(5, TimeUnit.SECONDS))
            assertEquals(QualityCounterId.REASON_SIZE_LIMIT, reason.get())
            val file = root.walkTopDown().filter { it.isFile && it.extension == "jhlog" }.single()
            assertTrue(file.length() <= 12 * 1024)
            assertTrue(file.length() > ProcessRecordingSession.MAGIC.size)
        } finally { writer.close(); recording.close(); root.deleteRecursively() }
    }

    @Test
    fun smallerPolicyNeverTruncatesExistingEpochOrCreatesAnotherFile() {
        val root = Files.createTempDirectory("jh-policy-reconfigure-cap").toFile()
        val recording = ProcessRecordingSession()
        val factory = AsyncLogWriterFactory(recording)
        try {
            factory.open(root, config(root, 100_000), "main").useWriter { it.counter("before", 1) }
            val log = root.walkTopDown().filter { it.isFile && it.extension == "jhlog" }.single()
            val previous = log.readBytes()
            val terminal = CountDownLatch(1)
            val reason = AtomicInteger()
            val next = factory.open(root, config(root, previous.size.toLong()), "main",
                onTerminalStop = { _, value, _ -> reason.set(value); terminal.countDown() })
            next.useWriter { it.counter("after", 1) }
            assertTrue(terminal.await(5, TimeUnit.SECONDS))
            assertEquals(QualityCounterId.REASON_SIZE_LIMIT, reason.get())
            assertArrayEquals(previous, log.readBytes())
            assertEquals(1, root.walkTopDown().count { it.isFile && it.extension == "jhlog" })
        } finally { recording.close(); root.deleteRecursively() }
    }

    @Test
    fun recordingMagicIsChargedWithOtherUnflushedWriterClaims() {
        val root = Files.createTempDirectory("jh-policy-recording-magic").toFile()
        val recording = ProcessRecordingSession()
        val config = config(root, 100_000)
        try {
            SessionStorageBudget.open(root, checkNotNull(config.storagePolicy())).use { first ->
                first.claim(5_000, terminal = false)
                AsyncLogWriterFactory(recording).open(root, config, "main").useWriter { it.counter("value", 1) }
                val file = root.walkTopDown().filter { it.isFile && it.extension == "jhlog" }.single()
                val overQuota = 1_000_000L - RunArchiveBudget.TERMINAL_RESERVE_BYTES - 5_000L - file.length() + 1L
                try {
                    first.claim(overQuota, terminal = false)
                    throw AssertionError("recording envelope must count even when another writer has unflushed claims")
                } catch (expected: StorageBudgetExhaustedException) {
                    assertEquals(1_000_000L, expected.limitBytes)
                }
            }
        } finally { recording.close(); root.deleteRecursively() }
    }

    private fun config(root: File, fileLimit: Long, exemptions: Set<String> = setOf("hprof")): JankHunterConfig = JankHunterConfig.builder()
        .autoStartCollectors(false).sessionLogSizeLimitEnabled(false).flushIntervalMs(1)
        .storagePolicy(JankHunterStoragePolicy(root, fileLimit, 1_000_000, setOf("jhlog", "hprof"), exemptions, 1024, true))
        .build()

    private fun AsyncLogWriter.useWriter(block: (AsyncLogWriter) -> Unit) {
        try { block(this) } finally { assertTrue(close()) }
    }
}
