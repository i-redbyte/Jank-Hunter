package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterBinaryStorage
import io.jankhunter.runtime.JankHunterConfig
import java.io.File

/** Applies one retention policy to local and externally delegated session storage. */
internal class SessionLogRetentionCoordinator(
    private val directory: File,
    private val config: JankHunterConfig,
    private val quality: LogQualityCounters,
) {
    fun enforce(activeWriter: BinaryLogWriter?, storage: JankHunterBinaryStorage?, runId: ByteArray) {
        val writer = activeWriter ?: return
        try {
            val leasePaths = SessionLogAllocator.activeLeases(directory).protectedPaths
            val result = if (storage == null) {
                SessionLogRetention.enforce(
                    directory = directory,
                    currentRunId = SessionLogName.runIdHex(runId),
                    protectedPaths = leasePaths,
                    historyLimitBytes = config.sessionLogSizeLimitBytes(),
                )
            } else {
                SessionLogRetention.enforce(
                    storage = storage,
                    currentRunId = SessionLogName.runIdHex(runId),
                    protectedPaths = leasePaths + writer.path,
                    historyLimitBytes = effectiveArchiveLimitBytes(config, storage),
                )
            }
            recordLogRetention(quality, result)
        } catch (error: Throwable) {
            if (error is VirtualMachineError || error is ThreadDeath) throw error
        }
    }
}
