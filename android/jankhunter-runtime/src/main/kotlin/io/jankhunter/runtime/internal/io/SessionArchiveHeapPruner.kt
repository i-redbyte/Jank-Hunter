package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterStoragePolicy
import io.jankhunter.runtime.internal.system.RetainedHeapDumper
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.util.zip.ZipEntry
import java.util.zip.ZipFile
import java.util.zip.ZipOutputStream

/** Called under the archive coordinator lock. Old HPROF payloads are never read or copied. */
internal fun pruneHistoricalArchiveHeapDumps(
    root: File,
    keepRunId: String,
    policy: JankHunterStoragePolicy? = null,
    verify: (File, SessionArtifactPath.ParsedSession) -> Boolean,
): Long = if (policy == null) {
    SessionArtifactReadLeases.mutate(root, 0L) { pruneHistoricalArchiveHeapDumpsUnleased(root, keepRunId, verify, null) }
} else {
    pruneHistoricalArchiveHeapDumpsUnleased(root, keepRunId, verify, policy)
}

private fun pruneHistoricalArchiveHeapDumpsUnleased(
    root: File,
    keepRunId: String,
    verify: (File, SessionArtifactPath.ParsedSession) -> Boolean,
    policy: JankHunterStoragePolicy?,
): Long {
    var failures = 0L
    root.listFiles { file -> file.isFile && file.name.endsWith(".jhlog.zip") }.orEmpty().forEach { archive ->
        val identity = SessionArtifactPath.parseSessionDirectoryName(archive.name.removeSuffix(".jhlog.zip"))
            ?: return@forEach
        if (identity.runId == keepRunId) return@forEach
        try {
            pruneArchiveHeapDumps(archive, identity, verify, policy)
        } catch (error: Throwable) {
            if (error is VirtualMachineError || error is ThreadDeath) throw error
            failures++
        }
    }
    return failures
}

private fun pruneArchiveHeapDumps(
    archive: File,
    identity: SessionArtifactPath.ParsedSession,
    verify: (File, SessionArtifactPath.ParsedSession) -> Boolean,
    policy: JankHunterStoragePolicy?,
) {
    val root = checkNotNull(archive.parentFile)
    val temporary = File.createTempFile(".jh-heap-prune-", ".tmp", root)
    val modified = archive.lastModified()
    val length = archive.length()
    try {
        val lease = if (policy == null) null else SessionArtifactReadLeases.acquire(root)
        lease.use {
        ZipFile(archive).use { zip ->
            if (zip.entries().asSequence().none { isHeapEntry(it) }) return
            FileOutputStream(temporary).use { fileOutput ->
                ZipOutputStream(fileOutput.buffered(COPY_BUFFER_BYTES)).use { output ->
                    val buffer = ByteArray(COPY_BUFFER_BYTES)
                    val entries = zip.entries()
                    var count = 0
                    while (entries.hasMoreElements()) {
                        val entry = entries.nextElement()
                        if (++count > MAX_ENTRIES) throw IOException("Jank Hunter archive entry limit exceeded")
                        if (isHeapEntry(entry)) continue
                        // Retain the original CRC/size: STORED output verifies copied content on closeEntry.
                        if (entry.method != ZipEntry.STORED) throw IOException("Unexpected session archive compression")
                        output.putNextEntry(ZipEntry(entry))
                        zip.getInputStream(entry).use { input ->
                            while (true) {
                                val size = input.read(buffer)
                                if (size < 0) break
                                if (size > 0) output.write(buffer, 0, size)
                            }
                        }
                        output.closeEntry()
                    }
                    output.finish()
                    output.flush()
                    fileOutput.fd.sync()
                }
            }
        }
        if (!verify(temporary, identity)) throw IOException("Cannot verify Jank Hunter archive without old heap dumps")
        }
        val replaced = if (policy == null) temporary.renameTo(archive) else
            SessionStorageBudget.replaceArchive(root, policy, temporary, archive, length, modified)
        if (!replaced) throw IOException("Cannot replace Jank Hunter archive atomically")
        archive.setLastModified(modified)
    } finally {
        temporary.delete()
    }
}

private fun isHeapEntry(entry: ZipEntry): Boolean {
    val separator = entry.name.indexOf('/')
    return !entry.isDirectory && separator > 0 && separator == entry.name.lastIndexOf('/') &&
        SessionArtifactPath.isCanonicalId(entry.name.substring(0, separator)) &&
        RetainedHeapDumper.isManagedHeapDumpFileName(entry.name.substring(separator + 1))
}

private const val COPY_BUFFER_BYTES = 64 * 1024
private const val MAX_ENTRIES = 100_000
