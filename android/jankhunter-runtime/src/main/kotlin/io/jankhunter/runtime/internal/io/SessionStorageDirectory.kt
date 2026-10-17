package io.jankhunter.runtime.internal.io

import java.io.Closeable
import java.io.DataInputStream
import java.io.DataOutputStream
import java.io.File
import java.io.FileOutputStream
import java.io.IOException

/** Cold-path directory selection; all processes keep the root of the currently active cohort. */
internal class SessionStorageDirectory(private val stateDirectory: File) : Closeable {
    private var selected: File? = null
    private var requestedPath: File? = null
    private var fallbackPath: File? = null
    private var selectionLease: Closeable? = null
    private var closed = false

    @Synchronized
    fun resolve(fallback: File, requested: File?): File {
        check(!closed) { "Jank Hunter directory selection is closed" }
        val canonicalFallback = fallback.canonicalFile
        val canonicalRequested = requested?.canonicalFile
        val current = selected
        if (current != null && canonicalRequested == requestedPath && canonicalFallback == fallbackPath) return current
        if (!stateDirectory.isDirectory && !stateDirectory.mkdirs() && !stateDirectory.isDirectory) {
            throw IOException("Cannot create Jank Hunter directory state")
        }
        return CrossProcessFileLocks.withDirectoryLock(stateDirectory, LOCK_FILE) {
            var previous = readState()
            previous = finishPendingCleanup(previous)
            val desired = canonicalRequested ?: previous?.takeIf { it.fallback == canonicalFallback }?.requested ?: canonicalFallback
            val previousRoot = previous?.current ?: canonicalFallback
            // Persist the destination before preflight, so a crash before commit can resume using
            // the same destination even when host policy arrives late on the next launch.
            val intent = DirectoryState(previousRoot, desired, canonicalFallback)
            if (intent != previous) {
                writeState(intent)
                previous = intent
            }
            val root = current ?: SessionArtifactReadLeases.mutate(stateDirectory, previousRoot) {
                chooseRoot(previousRoot, desired, canonicalFallback)
            }
            val next = DirectoryState(root, desired, canonicalFallback)
            if (next != previous) writeState(next)
            // Acquired before releasing the selection mutex, so another process cannot migrate
            // between this resolution and the first writer joining its process cohort.
            if (selectionLease == null) selectionLease = SessionArtifactReadLeases.acquire(stateDirectory)
            selected = root
            requestedPath = canonicalRequested
            fallbackPath = canonicalFallback
            root
        }
    }

    private fun finishPendingCleanup(previous: DirectoryState?): DirectoryState? {
        val source = previous?.cleanupRoot ?: return previous
        if (!SessionStorageMigration.finishCleanup(source, previous.current)) {
            throw IOException("Jank Hunter previous storage cleanup is still in use")
        }
        return previous.copy(cleanupRoot = null).also(::writeState)
    }

    private fun chooseRoot(previous: File, desired: File, fallback: File): File {
        if (previous == desired || ProcessRunCohort.activeRunIds(previous).isNotEmpty()) return previous
        var published = false
        return try {
            val migrated = SessionStorageMigration.migrate(previous, desired) {
                writeState(DirectoryState(desired, desired, fallback, cleanupRoot = previous))
                published = true
            }
            if (migrated) desired else previous
        } catch (error: IOException) {
            // Before journal publication the old root remains authoritative. After publication keep
            // the migration marker and fail closed; the next launch resumes artifact transfers.
            if (published) throw error
            previous
        }
    }

    private fun readState(): DirectoryState? {
        val file = File(stateDirectory, STATE_FILE)
        if (!file.isFile || file.length() !in 1L..MAX_STATE_BYTES) return null
        return try {
            DataInputStream(file.inputStream().buffered()).use { input ->
                val magic = input.readInt()
                if (magic != MAGIC && magic != LEGACY_MAGIC) return null
                val current = File(input.readUTF())
                val requested = File(input.readUTF())
                val fallback = if (magic == MAGIC) File(input.readUTF()) else current
                val cleanup = if (magic == MAGIC) input.readUTF().takeIf(String::isNotEmpty)?.let(::File) else null
                if (!current.isAbsolute || !requested.isAbsolute || !fallback.isAbsolute ||
                    cleanup != null && !cleanup.isAbsolute || input.read() != -1
                ) return null
                DirectoryState(current.canonicalFile, requested.canonicalFile, fallback.canonicalFile, cleanup?.canonicalFile)
            }
        } catch (_: IOException) { null }
    }

    private fun writeState(state: DirectoryState) {
        val pending = File(stateDirectory, "$STATE_FILE.pending")
        try {
            FileOutputStream(pending).use { stream ->
                val output = DataOutputStream(stream)
                output.writeInt(MAGIC)
                output.writeUTF(state.current.path)
                output.writeUTF(state.requested.path)
                output.writeUTF(state.fallback.path)
                output.writeUTF(state.cleanupRoot?.path.orEmpty())
                output.flush()
                stream.fd.sync()
            }
            if (!pending.renameTo(File(stateDirectory, STATE_FILE))) {
                throw IOException("Cannot publish Jank Hunter directory state")
            }
        } finally { pending.delete() }
    }

    @Synchronized
    override fun close() {
        if (closed) return
        closed = true
        selectionLease?.close()
        selectionLease = null
    }

    private data class DirectoryState(val current: File, val requested: File, val fallback: File, val cleanupRoot: File? = null)

    private companion object {
        const val MAGIC = 0x4a485345
        const val LEGACY_MAGIC = 0x4a485344
        const val MAX_STATE_BYTES = 32_768L
        const val LOCK_FILE = ".storage-directory.lock"
        const val STATE_FILE = "storage-directory.bin"
    }
}
