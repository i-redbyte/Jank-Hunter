package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterStoragePolicy
import io.jankhunter.runtime.internal.system.RetainedHeapDumper
import java.io.File
import java.io.IOException

/** One host quota for committed files across every process and retained session. */
internal object SessionStorageBudget {
    private const val METADATA_DIRECTORY = ".jh-storage-quota"
    private const val QUOTA_ID = "00000000000000000000000000000001"

    fun open(root: File, policy: JankHunterStoragePolicy): RunArchiveBudget =
        open(root, policy, RunArchiveBudget.TERMINAL_RESERVE_BYTES)

    private fun open(root: File, policy: JankHunterStoragePolicy, reservationBytes: Long): RunArchiveBudget {
        val metadata = File(root, METADATA_DIRECTORY)
        return RunArchiveBudget.open(
            metadata,
            QUOTA_ID,
            policy.archivesSizeLimitBytes,
            actualArchiveBytes = { physicalBytes(root) },
            reclaimBytesTo = { remaining -> reclaim(root, remaining) },
            sharedLimit = true,
            reservationBytes = reservationBytes,
            reconcileOnOpen = true,
        )
    }

    /** The pending file is excluded from committed quota until an atomic publish succeeds. */
    fun publishHeapDump(root: File, policy: JankHunterStoragePolicy, pending: File, destination: File): Boolean {
        val size = pending.length()
        if (!policy.allowsExtension("hprof") || size <= 0L || size > policy.fileLimitBytes("hprof") ||
            size > policy.archivesSizeLimitBytes || !pending.isFile || destination.exists()
        ) return false
        val process = destination.parentFile?.canonicalFile ?: return false
        if (!isProcessDirectory(root.canonicalFile, process) || pending.canonicalFile.parentFile != process ||
            !RetainedHeapDumper.isManagedHeapDumpFileName(destination.name)
        ) return false
        return try {
            open(root, policy, reservationBytes = 0L).use { budget ->
                budget.claim(size, terminal = true)
                val published = try {
                    SessionArtifactReadLeases.publish(root) {
                        !destination.exists() && pending.renameTo(destination)
                    }
                } catch (error: Throwable) {
                    budget.releaseClaim(size)
                    throw error
                }
                if (!published) {
                    budget.releaseClaim(size)
                    throw IOException("Cannot publish managed heap dump")
                }
            }
            true
        } catch (_: StorageBudgetExhaustedException) {
            false
        }
    }

    fun publishArchive(
        root: File,
        policy: JankHunterStoragePolicy,
        temporary: File?,
        archive: File,
        session: File,
        publishAndDelete: () -> Boolean,
    ): Boolean = RunArchiveBudget.mutateAccountedFiles(
        File(root, METADATA_DIRECTORY), QUOTA_ID, policy.archivesSizeLimitBytes,
        actualBytes = { physicalBytes(root) },
        requestedBytes = temporary?.length() ?: 0L,
        trackedBytes = { saturatedAdd(sessionBytes(session), archive.length()) },
    ) { SessionArtifactReadLeases.mutate(root, false, publishAndDelete) }

    fun replaceArchive(
        root: File,
        policy: JankHunterStoragePolicy,
        temporary: File,
        archive: File,
        expectedLength: Long,
        expectedModified: Long,
    ): Boolean = RunArchiveBudget.mutateAccountedFiles(
        File(root, METADATA_DIRECTORY), QUOTA_ID, policy.archivesSizeLimitBytes,
        actualBytes = { physicalBytes(root) },
        requestedBytes = (temporary.length() - expectedLength).coerceAtLeast(0L),
        trackedBytes = archive::length,
    ) {
        SessionArtifactReadLeases.mutate(root, false) {
            archive.length() == expectedLength && archive.lastModified() == expectedModified && temporary.renameTo(archive)
        }
    }

    private fun sessionBytes(session: File): Long {
        var bytes = 0L
        for (process in session.listFiles().orEmpty()) {
            if (!process.isDirectory || process.canonicalFile.parentFile != session.canonicalFile) continue
            for (file in process.listFiles().orEmpty()) {
                if (file.isFile && file.canonicalFile.parentFile == process.canonicalFile &&
                    (SessionLogName.parse(file.name) != null || RetainedHeapDumper.isManagedHeapDumpFileName(file.name))
                ) bytes = saturatedAdd(bytes, file.length())
            }
        }
        return bytes
    }

    /** Recovery frees completed artifacts; a partially written chunk is never blindly replayed. */
    fun recoverDiskFull(root: File, policy: JankHunterStoragePolicy, failure: Throwable): Long {
        if (!policy.handleDiskFull || !isDiskFull(failure)) return 0L
        return open(root, policy, reservationBytes = 0L).use { budget ->
            val target = (physicalBytes(root) - maxOf(policy.bufferSize.toLong(), RunArchiveBudget.TERMINAL_RESERVE_BYTES))
                .coerceAtLeast(0L)
            budget.reclaimTo(target)
        }
    }

    private fun isDiskFull(failure: Throwable): Boolean {
        var current: Throwable? = failure
        repeat(16) {
            val error = current ?: return false
            val message = error.message.orEmpty()
            if (message.contains("ENOSPC") || message.contains("No space left on device", ignoreCase = true)) return true
            current = error.cause
        }
        return false
    }

    /** A live export defers retention successfully; only actual deletion releases quota. */
    fun deleteHeapDump(root: File, file: File): Boolean {
        val process = file.parentFile?.canonicalFile ?: return false
        if (!isProcessDirectory(root.canonicalFile, process) || file.canonicalFile.parentFile != process ||
            !RetainedHeapDumper.isManagedHeapDumpFileName(file.name)
        ) return false
        return RunArchiveBudget.deleteAccountedFile(File(root, METADATA_DIRECTORY), QUOTA_ID, file) {
            SessionArtifactReadLeases.mutate(root, true) { !file.exists() || file.delete() }
        }
    }

    /** Cold metadata scan. It never opens a heap payload or a log. */
    fun physicalBytes(root: File): Long {
        var total = 0L
        for (file in managedFiles(root)) total = saturatedAdd(total, file.length().coerceAtLeast(0L))
        return total
    }

    private fun reclaim(root: File, targetBytes: Long): Long =
        SessionArtifactReadLeases.mutate(root, 0L) { reclaimUnleased(root, targetBytes) }

    private fun reclaimUnleased(root: File, targetBytes: Long): Long {
        val before = physicalBytes(root)
        if (before <= targetBytes) return 0L
        var remaining = before
        val heaps = managedFiles(root).filter { file -> RetainedHeapDumper.isManagedHeapDumpFileName(file.name) }
            .sortedBy(File::getName)
        for (heap in heaps) {
            if (remaining <= targetBytes) break
            val size = heap.length()
            if (heap.delete()) remaining = (remaining - size).coerceAtLeast(0L)
        }
        if (remaining > targetBytes) {
            // Existing archives may still carry HPROF from an older SDK. Strip it before logs.
            pruneHistoricalArchiveHeapDumps(root, QUOTA_ID, verify = SessionArchiveCoordinator::verifyPublishedArchive)
            remaining = physicalBytes(root)
        }
        if (remaining > targetBytes) {
            var archiveBytes = 0L
            for (file in managedFiles(root)) {
                if (file.name.endsWith(".jhlog.zip")) archiveBytes = saturatedAdd(archiveBytes, file.length())
            }
            val nonArchiveBytes = (remaining - archiveBytes).coerceAtLeast(0L)
            SessionArchiveRetention.enforce(
                root, (targetBytes - nonArchiveBytes).coerceAtLeast(0L), preserveNewest = false,
            )
            remaining = physicalBytes(root)
        }
        if (remaining > targetBytes) {
            val active = ProcessRunCohort.activeRunIds(root)
            val sessions = root.listFiles { file -> file.isDirectory &&
                SessionArtifactPath.parseSessionDirectoryName(file.name) != null }.orEmpty().sortedBy(File::getName)
            for (session in sessions) {
                if (remaining <= targetBytes) break
                val identity = checkNotNull(SessionArtifactPath.parseSessionDirectoryName(session.name))
                if (identity.runId in active || session.canonicalFile.parentFile != root.canonicalFile) continue
                val size = sessionBytes(session)
                if (SessionArchiveCoordinator.reclaimCompletedSession(session, identity)) {
                    remaining = (remaining - size).coerceAtLeast(0L)
                }
            }
        }
        return (before - remaining).coerceAtLeast(0L)
    }

    private fun managedFiles(root: File): List<File> {
        val canonicalRoot = root.canonicalFile
        val files = ArrayList<File>()
        for (entry in canonicalRoot.listFiles().orEmpty()) {
            if (entry.canonicalFile.parentFile != canonicalRoot) continue
            if (entry.isFile && entry.name.endsWith(".jhlog.zip") &&
                SessionArtifactPath.parseSessionDirectoryName(entry.name.removeSuffix(".jhlog.zip")) != null
            ) {
                files += entry
            } else if (entry.isDirectory && SessionArtifactPath.parseSessionDirectoryName(entry.name) != null) {
                for (process in entry.listFiles().orEmpty()) {
                    if (!isProcessDirectory(canonicalRoot, process)) continue
                    for (file in process.listFiles().orEmpty()) {
                        if (file.isFile && file.canonicalFile.parentFile == process.canonicalFile &&
                            (SessionLogName.parse(file.name) != null || RetainedHeapDumper.isManagedHeapDumpFileName(file.name))
                        ) files += file
                    }
                }
            }
        }
        return files
    }

    private fun isProcessDirectory(root: File, process: File): Boolean {
        val session = process.parentFile ?: return false
        return process.isDirectory && SessionArtifactPath.isCanonicalId(process.name) &&
            SessionArtifactPath.parseSessionDirectoryName(session.name) != null &&
            session.canonicalFile.parentFile == root && process.canonicalFile.parentFile == session.canonicalFile
    }

    private fun saturatedAdd(first: Long, second: Long): Long =
        if (second > Long.MAX_VALUE - first) Long.MAX_VALUE else first + second
}
