package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterLogSnapshot
import java.io.File
import java.io.IOException

/** Compatibility copies for callers of the file-path snapshot API. ZIP export reads frontiers directly. */
internal object SnapshotFileCache {
    fun materialize(snapshot: JankHunterLogSnapshot, root: File, budgetBytes: Long): JankHunterLogSnapshot {
        if (snapshot.logByteLimits.isEmpty()) return snapshot
        if (!root.isDirectory && !root.mkdirs()) throw IOException("Cannot create Jank Hunter snapshot cache")
        return CrossProcessFileLocks.withDirectoryLock(root, ".snapshot-cache.lock") {
            root.listFiles { file ->
                file.isDirectory && file.name.startsWith(".") && file.name.endsWith(".tmp") &&
                    SessionArtifactPath.isCanonicalId(file.name.removePrefix(".").removeSuffix(".tmp"))
            }.orEmpty().forEach { abandoned ->
                if (!abandoned.deleteRecursively()) throw IOException("Cannot remove abandoned Jank Hunter snapshot")
            }
            materializeLocked(snapshot, root, budgetBytes)
        }
    }

    private fun materializeLocked(snapshot: JankHunterLogSnapshot, root: File, budgetBytes: Long): JankHunterLogSnapshot {
        val target = File(root, SessionLogName.runIdHex(BinaryLogFileHeader.randomId()))
        val temporary = File(root, ".${target.name}.tmp")
        if (!temporary.mkdir()) throw IOException("Cannot create Jank Hunter snapshot copy")
        var published = false
        try {
            val buffer = ByteArray(COPY_BUFFER_BYTES)
            val paths = ArrayList<String>(snapshot.logPaths.size)
            snapshot.logPaths.forEachIndexed { index, path ->
                val process = File(temporary, index.toString())
                if (!process.mkdir()) throw IOException("Cannot create Jank Hunter snapshot process directory")
                val source = File(path)
                val copy = File(process, source.name)
                ArtifactFrontierInputStream(source, snapshot.byteLimit(index)).use { input ->
                    copy.outputStream().use { output ->
                        while (true) {
                            val count = input.read(buffer)
                            if (count < 0) break
                            if (count > 0) output.write(buffer, 0, count)
                        }
                    }
                }
                paths += File(File(target, index.toString()), source.name).absolutePath
            }
            if (!temporary.renameTo(target)) throw IOException("Cannot publish Jank Hunter snapshot copy")
            published = true
            enforceBudget(root, target, budgetBytes.takeIf { it > 0L } ?: DEFAULT_BUDGET_BYTES)
            return JankHunterLogSnapshot(snapshot.capturedAtMs, paths, snapshot.processCount, snapshot.captureSkewMs,
                release = { target.deleteRecursively() })
        } finally {
            if (!published) temporary.deleteRecursively()
        }
    }

    private fun enforceBudget(root: File, current: File, budget: Long) {
        val directories = root.listFiles { file -> file.isDirectory && SessionArtifactPath.isCanonicalId(file.name) }
            .orEmpty().sortedBy(File::lastModified)
        val sizes = LongArray(directories.size)
        var total = 0L
        directories.forEachIndexed { index, directory ->
            val size = directory.walkTopDown().filter(File::isFile).sumOf(File::length)
            sizes[index] = size
            total = saturatedAdd(total, size)
        }
        directories.forEachIndexed { index, directory ->
            if (total > budget && directory != current && directory.deleteRecursively()) total -= sizes[index]
        }
    }

    private const val COPY_BUFFER_BYTES = 64 * 1024
    private const val DEFAULT_BUDGET_BYTES = 64L * 1024L * 1024L
}
