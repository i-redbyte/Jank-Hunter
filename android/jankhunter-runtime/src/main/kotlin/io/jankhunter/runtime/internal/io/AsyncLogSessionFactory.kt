package io.jankhunter.runtime.internal.io

import android.os.Process
import io.jankhunter.runtime.JankHunterBinaryArtifact
import io.jankhunter.runtime.JankHunterBinaryStorage
import io.jankhunter.runtime.JankHunterBinaryWriter
import io.jankhunter.runtime.JankHunterConfig
import java.io.File
import java.io.IOException
import java.nio.charset.StandardCharsets
import java.security.MessageDigest
import java.util.TimeZone

/** Creates one physical JHLOG segment and owns all segment-opening policy. */
internal class AsyncLogSessionFactory(
    private val directory: File,
    private val config: JankHunterConfig,
    private val processName: String,
    private val expectedProcesses: Set<String>,
    private val rosterDeclarationComplete: Boolean,
    private val collectorStartElapsedUs: Long,
    private val quality: LogQualityCounters,
    private val logGrowthManager: LogGrowthManager?,
) {
    fun open(
        localDate: String,
        dailySessionIndex: Long,
        runId: ByteArray,
        sessionId: ByteArray,
        segmentIndex: Long,
        previousSegmentDigest: ByteArray,
        baseStats: LogContainerStats,
        segmentStartElapsedUs: Long,
        segmentStartUnixMs: Long,
        storage: JankHunterBinaryStorage?,
    ): OpenedLogSession {
        val archiveLimit = effectiveArchiveLimitBytes(config, storage)
        val physicalLimit = storage?.fileSizeLimitBytes
            ?.takeIf { limit -> limit < Long.MAX_VALUE }
            ?: 0L
        val exactAdmission = config.exactEventCollectionEnabled()
        // Exact admission may wait for bounded queues to drain, but it must never turn memory
        // limits into sizing hints. Dictionary overflow remains explicit in quality counters.
        val dictionaryEntries = config.maxDictionaryEntries()
        val dictionaryValueBytes = config.maxDictionaryValueBytes()
        val allowedProcesses = config.allowedProcesses()
        val allowedProcessCount = allowedProcesses.size.toLong()
        val processScope = when {
            allowedProcessCount > 0L -> Jhlog.PROCESS_SCOPE_ALLOWLIST
            config.mainProcessOnly() -> Jhlog.PROCESS_SCOPE_MAIN_ONLY
            else -> Jhlog.PROCESS_SCOPE_ALL
        }
        val header = BinaryLogFileHeader(
            runId = runId,
            processInstanceId = PROCESS_INSTANCE_ID,
            sessionId = sessionId,
            segmentIndex = segmentIndex,
            osPid = Process.myPid().toLong().coerceAtLeast(0L),
            collectorStartElapsedUs = collectorStartElapsedUs,
            segmentStartElapsedUs = segmentStartElapsedUs,
            segmentStartUnixMs = segmentStartUnixMs,
            timezoneOffsetMinutes = TimeZone.getDefault().getOffset(segmentStartUnixMs) / 60_000L,
            identitySource = 0L,
            processName = processName,
            symbolNamespace = config.symbolNamespace(),
            processScope = processScope,
            allowedProcessCount = allowedProcessCount,
            processScopeFingerprint = processScopeFingerprint(allowedProcesses),
            previousSegmentDigest = previousSegmentDigest,
            expectedProcessCount = expectedProcesses.size.toLong(),
            expectedProcessFingerprint = processScopeFingerprint(expectedProcesses),
            processRosterDeclarationComplete = rosterDeclarationComplete,
            requiredFeatures = if (exactAdmission) {
                Jhlog.REQUIRED_FEATURES
            } else {
                Jhlog.BEST_EFFORT_FEATURES
            },
        )
        val logGrowth = logGrowthManager?.let { manager ->
            LogGrowthSessionBinding(manager, localDate, archiveLimit, baseStats)
        }
        var attempts = 0
        var minimumSegmentIndex = 0L
        while (attempts < MAX_OPEN_ATTEMPTS) {
            attempts++
            val authoritativeStoragePaths = storage?.let { binaryStorage ->
                runCatching { binaryStorage.listFiles() }.getOrNull()
            }
            val allocation = SessionLogAllocator.reserve(
                directory = directory,
                localDate = localDate,
                runId = runId,
                dailySessionIndex = dailySessionIndex,
                authoritativeStoragePaths = authoritativeStoragePaths,
                minimumSegmentIndex = minimumSegmentIndex,
            )
            var localFile: File? = null
            var externalWriter: JankHunterBinaryWriter? = null
            var protection: JankHunterBinaryArtifact? = null
            var archiveBudget: RunArchiveBudget? = null
            var binaryWriter: BinaryLogWriter? = null
            try {
                if (storage == null) {
                    val candidate = File(directory, allocation.fileName)
                    if (!candidate.createNewFile()) {
                        minimumSegmentIndex = nextSegmentIndex(allocation.segmentIndex)
                        allocation.close()
                        continue
                    }
                    localFile = candidate
                    archiveBudget = openArchiveBudget(storage, runId, archiveLimit)
                    binaryWriter = BinaryLogWriter(
                        candidate,
                        dictionaryEntries,
                        dictionaryValueBytes,
                        header,
                        quality,
                        physicalLimit,
                        logGrowth = logGrowth,
                        archiveBudget = archiveBudget,
                    )
                } else {
                    val candidate = storage.openWriter(allocation.fileName)
                    externalWriter = candidate
                    if (candidate.bytesWritten() != 0L) {
                        minimumSegmentIndex = nextSegmentIndex(allocation.segmentIndex)
                        runCatching { candidate.close() }
                        allocation.close()
                        continue
                    }
                    archiveBudget = openArchiveBudget(storage, runId, archiveLimit)
                    binaryWriter = BinaryLogWriter(
                        candidate,
                        dictionaryEntries,
                        dictionaryValueBytes,
                        header,
                        quality,
                        physicalLimit,
                        logGrowth,
                        archiveBudget,
                    )
                    protection = storage.protect(allocation.fileName)
                }
                val openedWriter = checkNotNull(binaryWriter)
                allocation.updateProtectedPath(openedWriter.path)
                return OpenedLogSession(allocation, openedWriter, protection)
            } catch (error: Throwable) {
                runCatching { protection?.abort() }
                binaryWriter?.abort() ?: runCatching { externalWriter?.close() }
                runCatching { archiveBudget?.close() }
                if (binaryWriter == null && externalWriter != null) {
                    runCatching { storage?.delete(allocation.fileName) }
                }
                allocation.close()
                localFile?.delete()
                throw error
            }
        }
        throw IOException("Cannot allocate an unused Jank Hunter session log name")
    }

    private fun openArchiveBudget(
        storage: JankHunterBinaryStorage?,
        runId: ByteArray,
        archiveLimit: Long,
    ): RunArchiveBudget? {
        if (archiveLimit <= 0L || archiveLimit == Long.MAX_VALUE) return null
        val runIdHex = SessionLogName.runIdHex(runId)
        fun paths(): List<String> = storage?.listFiles() ?: directory.listFiles { file -> file.isFile }
            .orEmpty()
            .map(File::getAbsolutePath)
        return RunArchiveBudget.open(
            directory = directory,
            runId = runIdHex,
            limitBytes = archiveLimit,
            actualArchiveBytes = { RunArchiveBudget.retainedJhlogBytes(paths()) },
            reclaimBytesTo = { targetBytes ->
                val protectedPaths = SessionLogAllocator.activeLeases(directory).protectedPaths
                val result = if (storage == null) {
                    SessionLogRetention.enforce(directory, runIdHex, protectedPaths, targetBytes)
                } else {
                    SessionLogRetention.enforce(storage, runIdHex, protectedPaths, targetBytes)
                }
                recordLogRetention(quality, result)
                result.deletedBytes
            },
        )
    }

    private fun nextSegmentIndex(current: Long): Long {
        if (current == Long.MAX_VALUE) throw IOException("Jank Hunter session log segment index exhausted")
        return current + 1L
    }

    companion object {
        private const val MAX_OPEN_ATTEMPTS = 1_024
        private const val PROCESS_SCOPE_FINGERPRINT_BYTES = 32
        private val PROCESS_INSTANCE_ID = BinaryLogFileHeader.randomId()

        internal fun processScopeFingerprint(allowedProcesses: Set<String>): ByteArray {
            if (allowedProcesses.isEmpty()) return ByteArray(0)
            val digest = MessageDigest.getInstance("SHA-256")
            val length = ByteArray(Int.SIZE_BYTES)
            allowedProcesses.sorted().forEach { allowedProcess ->
                val bytes = allowedProcess.toByteArray(StandardCharsets.UTF_8)
                length[0] = (bytes.size ushr 24).toByte()
                length[1] = (bytes.size ushr 16).toByte()
                length[2] = (bytes.size ushr 8).toByte()
                length[3] = bytes.size.toByte()
                digest.update(length)
                digest.update(bytes)
            }
            return digest.digest().copyOf(PROCESS_SCOPE_FINGERPRINT_BYTES)
        }
    }
}

internal class OpenedLogSession(
    val allocation: SessionLogAllocator.Allocation,
    val writer: BinaryLogWriter,
    val protection: JankHunterBinaryArtifact?,
)

internal fun effectiveArchiveLimitBytes(
    config: JankHunterConfig,
    storage: JankHunterBinaryStorage?,
): Long {
    val configured = config.sessionLogSizeLimitBytes()
    val storageLimit = storage?.archivesSizeLimitBytes?.takeIf { limit -> limit < Long.MAX_VALUE } ?: 0L
    if (configured <= 0L) return storageLimit
    if (storageLimit <= 0L) return configured
    return minOf(configured, storageLimit)
}

internal fun recordLogRetention(quality: LogQualityCounters, result: SessionLogRetention.Result) {
    quality.add(QualityCounterId.ARCHIVE_EVICTED_RUN_TOTAL, result.deletedRuns)
    quality.add(QualityCounterId.ARCHIVE_EVICTED_SEGMENT_TOTAL, result.deletedSegments)
    quality.add(QualityCounterId.ARCHIVE_EVICTED_BYTES_TOTAL, result.deletedBytes)
}
