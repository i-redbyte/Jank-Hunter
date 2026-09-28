package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.internal.system.RetainedHeapDumper
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.security.MessageDigest
import java.security.DigestInputStream

/** Cold migration journals its destination before atomically transferring any managed artifact. */
internal object SessionStorageMigration {
    fun migrate(source: File, destination: File, publishState: () -> Unit): Boolean =
        withRoots(source, destination) {
            val files = managedFiles(source) ?: return@withRoots false
            verifyCollisions(source, destination, files)
            // A crash after this point must resume transfers, never return to the old authority.
            publishState()
            transferFiles(source, destination, files)
            true
        }

    fun finishCleanup(source: File, destination: File): Boolean = withRoots(source, destination) {
        val files = managedFiles(source) ?: return@withRoots false
        verifyCollisions(source, destination, files)
        transferFiles(source, destination, files)
        true
    }

    private fun verifyCollisions(source: File, destination: File, files: List<File>) {
        for (file in files) {
            val target = destination.resolve(file.relativeTo(source))
            if (target.exists() && !sameContent(file, target)) throw IOException("Jank Hunter migration target conflicts")
        }
    }

    private fun withRoots(source: File, destination: File, action: () -> Boolean): Boolean {
        if (source == destination || source.toPath().startsWith(destination.toPath()) ||
            destination.toPath().startsWith(source.toPath())
        ) return false
        if (!destination.isDirectory && !destination.mkdirs() && !destination.isDirectory) return false
        val first = if (source.path < destination.path) source else destination
        val second = if (first == source) destination else source
        return SessionArtifactReadLeases.mutate(first, false) {
            SessionArtifactReadLeases.mutate(second, false) {
                if (ProcessRunCohort.activeRunIds(source).isNotEmpty() ||
                    ProcessRunCohort.activeRunIds(destination).isNotEmpty()
                ) false else action()
            }
        }
    }

    private fun managedFiles(root: File): List<File>? {
        if (!root.exists()) return emptyList()
        val result = ArrayList<File>()
        for (entry in root.listFiles() ?: return null) {
            val identity = SessionArtifactPath.parseSessionDirectoryName(entry.name.removeSuffix(".jhlog.zip"))
            val flatArtifact = SessionLogName.parse(entry.name) != null ||
                RetainedHeapDumper.isManagedHeapDumpFileName(entry.name) || isSequenceFile(entry.name)
            if (identity == null && !flatArtifact) continue
            if (entry.canonicalFile != entry.absoluteFile) return null
            if (entry.isFile && (flatArtifact || entry.name.endsWith(".jhlog.zip"))) {
                result += entry
            } else if (entry.isDirectory && identity != null) {
                if (!addSessionFiles(entry, identity, result)) return null
            } else return null
            if (result.size > MAX_FILES) return null
        }
        return result
    }

    private fun isSequenceFile(name: String): Boolean {
        if (!name.startsWith(".jh-session-index.") || !name.endsWith(".seq")) return false
        val date = name.removePrefix(".jh-session-index.").removeSuffix(".seq")
        return runCatching { SessionLogName.sequenceFileName(date) == name }.getOrDefault(false)
    }

    private fun addSessionFiles(session: File, identity: SessionArtifactPath.ParsedSession, files: MutableList<File>): Boolean {
        for (process in session.listFiles() ?: return false) {
            if (!process.isDirectory || !SessionArtifactPath.isCanonicalId(process.name) ||
                process.canonicalFile != process.absoluteFile
            ) return false
            for (file in process.listFiles() ?: return false) {
                val parsed = SessionLogName.parse(file.name)
                val knownLog = parsed != null && parsed.runId == identity.runId &&
                    parsed.dailySessionIndex == identity.dailySessionIndex
                if (!file.isFile || file.canonicalFile != file.absoluteFile ||
                    !knownLog && !RetainedHeapDumper.isManagedHeapDumpFileName(file.name)
                ) return false
                files += file
                if (files.size > MAX_FILES) return false
            }
        }
        return true
    }

    private fun copy(source: File, target: File) {
        val parent = checkNotNull(target.parentFile)
        if (!parent.isDirectory && !parent.mkdirs() && !parent.isDirectory) throw IOException("Cannot create migration directory")
        if (target.canonicalFile != target.absoluteFile) throw IOException("Migration target contains a symbolic link")
        if (target.exists()) {
            if (!sameContent(source, target)) throw IOException("Jank Hunter storage migration target conflicts")
            return
        }
        val pending = File(parent, ".${target.name}.migration-pending")
        if (pending.canonicalFile != pending.absoluteFile) throw IOException("Migration pending path contains a symbolic link")
        try {
            val copiedDigest = MessageDigest.getInstance("SHA-256")
            val expectedSize = source.length()
            DigestInputStream(source.inputStream().buffered(COPY_BUFFER_BYTES), copiedDigest).use { input ->
                FileOutputStream(pending).use { output ->
                    input.copyTo(output, COPY_BUFFER_BYTES)
                    output.fd.sync()
                }
            }
            if (source.length() != expectedSize || pending.length() != expectedSize ||
                !copiedDigest.digest().contentEquals(digest(pending)) || !pending.renameTo(target)
            ) {
                throw IOException("Cannot publish migrated Jank Hunter artifact")
            }
        } finally { pending.delete() }
    }

    private fun transferFiles(source: File, destination: File, files: List<File>) {
        for (file in files) {
            val target = destination.resolve(file.relativeTo(source))
            val parent = checkNotNull(target.parentFile)
            if (!parent.isDirectory && !parent.mkdirs() && !parent.isDirectory) throw IOException("Cannot create migration directory")
            if (target.canonicalFile != target.absoluteFile) throw IOException("Migration target contains a symbolic link")
            if (target.exists()) {
                // Existing copies were verified before the first transfer under both mutation locks.
                if (!file.delete()) throw IOException("Cannot remove migrated Jank Hunter source artifact")
            } else if (!file.renameTo(target)) {
                // External filesystems may not support rename across roots. Stream only that case.
                copy(file, target)
                if (!file.delete()) throw IOException("Cannot remove copied Jank Hunter source artifact")
            }
        }
        // Delete only empty known containers; foreign root files are never traversed or removed.
        for (session in source.listFiles().orEmpty()) {
            if (!session.isDirectory || SessionArtifactPath.parseSessionDirectoryName(session.name) == null) continue
            for (process in session.listFiles().orEmpty()) {
                if (process.isDirectory && SessionArtifactPath.isCanonicalId(process.name)) process.delete()
            }
            session.delete()
        }
    }

    private fun sameContent(first: File, second: File): Boolean = second.isFile &&
        second.canonicalFile == second.absoluteFile && first.length() == second.length() &&
        digest(first).contentEquals(digest(second))

    private fun digest(file: File): ByteArray {
        val digest = MessageDigest.getInstance("SHA-256")
        val buffer = ByteArray(COPY_BUFFER_BYTES)
        file.inputStream().use { input ->
            while (true) {
                val count = input.read(buffer)
                if (count < 0) break
                if (count > 0) digest.update(buffer, 0, count)
            }
        }
        return digest.digest()
    }

    private const val MAX_FILES = 4_096
    private const val COPY_BUFFER_BYTES = 64 * 1024
}
