package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterBinaryArtifact
import io.jankhunter.runtime.JankHunterBinaryStorage
import java.io.File
import java.io.IOException

/** Owns segment leases, protections and transactional migration between storage backends. */
internal class SessionSegmentLedger(
    private val directory: File,
    private val processName: String,
) {
    private val allocations = ArrayList<SessionLogAllocator.Allocation>()
    private val completedSegments = ArrayList<CompletedSegment>()
    private val protections = ArrayList<JankHunterBinaryArtifact>()
    private var externalSegmentRegistry: ExternalSegmentRegistry? = null
    private var activeAllocation: SessionLogAllocator.Allocation? = null
    private var activeProtection: JankHunterBinaryArtifact? = null

    fun register(opened: OpenedLogSession, storage: JankHunterBinaryStorage?) {
        if (storage != null) runCatching { externalSegments().add(opened.writer.path) }
        allocations += opened.allocation
        opened.protection?.let(protections::add)
        activeAllocation = opened.allocation
        activeProtection = opened.protection
    }

    fun seal(path: String, storage: JankHunterBinaryStorage?) {
        val allocation = activeAllocation
            ?: throw IOException("Jank Hunter active segment has no allocation")
        completedSegments += CompletedSegment(path, storage, allocation, activeProtection)
        activeAllocation = null
        activeProtection = null
    }

    fun completedPaths(): List<String> {
        return completedSegments.mapTo(ArrayList(completedSegments.size)) { segment -> segment.path }
    }

    fun prepareHandoff(target: JankHunterBinaryStorage?): PreparedHandoff {
        val current = ArrayList<SegmentMigration>(completedSegments.size)
        val recovered = ArrayList<RecoveredSegmentMigration>()
        try {
            completedSegments.forEachIndexed { index, segment ->
                if (segment.storage === target) return@forEachIndexed
                val imported = importSegment(segment.path, target)
                imported.protection?.let(protections::add)
                if (target != null) runCatching { externalSegments().add(imported.path) }
                current += SegmentMigration(index, segment, target, imported)
            }
            prepareRecoveredMigrations(target, recovered)
            return PreparedHandoff(current, recovered)
        } catch (error: Throwable) {
            rollbackHandoff(target, PreparedHandoff(current, recovered))
            throw error
        }
    }

    fun rollbackHandoff(target: JankHunterBinaryStorage?, handoff: PreparedHandoff) {
        handoff.recovered.asReversed().forEach { migration ->
            rollbackImport(target, migration.imported)
        }
        handoff.current.asReversed().forEach { migration ->
            rollbackImport(target, migration.imported)
        }
    }

    fun commitHandoff(handoff: PreparedHandoff) {
        handoff.current.forEach { migration ->
            val source = migration.source
            val sourcePath = source.path
            val sourceProtection = source.protection
            if (sourceProtection != null) {
                protections.remove(sourceProtection)
                runCatching { sourceProtection.commit() }
            }
            if (sourcePath != migration.imported.path) {
                if (source.storage == null) {
                    File(sourcePath).delete()
                } else {
                    runCatching { source.storage?.delete(File(sourcePath).name) }
                    if (!File(sourcePath).exists()) runCatching { externalSegments().remove(sourcePath) }
                }
            }
            source.path = migration.imported.path
            source.storage = migration.target
            source.protection = migration.imported.protection
            source.allocation.updateProtectedPath(migration.imported.path)
            completedSegments[migration.index] = source
        }
        handoff.recovered.forEach { migration ->
            val imported = migration.imported
            if (migration.sourcePath != imported.path) {
                val source = File(migration.sourcePath)
                val deleted = !source.exists() || source.delete()
                if (migration.registeredExternal && deleted) {
                    runCatching { externalSegments().remove(migration.sourcePath) }
                }
            }
            imported.protection?.let { protection ->
                protections.remove(protection)
                runCatching { protection.commit() }
            }
        }
    }

    fun finish() {
        protections.forEach { protection -> runCatching { protection.commit() } }
        protections.clear()
        allocations.forEach { allocation -> allocation.close() }
        allocations.clear()
        activeAllocation = null
        activeProtection = null
    }

    private fun prepareRecoveredMigrations(
        target: JankHunterBinaryStorage?,
        migrations: MutableList<RecoveredSegmentMigration>,
    ) {
        val completedPaths = completedSegments.mapTo(HashSet(completedSegments.size)) { segment ->
            normalizedPath(segment.path)
        }
        val protectedPaths = SessionLogAllocator.activeLeases(directory).protectedPaths
            .mapTo(HashSet<String>()) { path -> normalizedPath(path) }
        val sources = LinkedHashMap<String, Boolean>()
        directory.listFiles { file -> file.isFile && SessionLogName.parse(file.name) != null }
            .orEmpty()
            .forEach { file ->
                val path = normalizedPath(file.absolutePath)
                if (path !in completedPaths && path !in protectedPaths) sources[path] = false
            }
        runCatching { externalSegments().paths() }.getOrDefault(emptyList()).forEach { registeredPath ->
            val path = normalizedPath(registeredPath)
            if (path !in completedPaths && path !in protectedPaths && File(path).isFile) sources[path] = true
        }
        sources.forEach { (sourcePath, registeredExternal) ->
            if (target == null && !registeredExternal) return@forEach
            val imported = importSegment(sourcePath, target)
            imported.protection?.let(protections::add)
            if (target != null) runCatching { externalSegments().add(imported.path) }
            migrations += RecoveredSegmentMigration(sourcePath, registeredExternal, imported)
        }
    }

    private fun importSegment(
        sourcePath: String,
        target: JankHunterBinaryStorage?,
    ): SealedSegmentImporter.ImportResult {
        return if (target == null) {
            SealedSegmentImporter.import(sourcePath, directory)
        } else {
            SealedSegmentImporter.import(sourcePath, target)
        }
    }

    private fun rollbackImport(
        target: JankHunterBinaryStorage?,
        imported: SealedSegmentImporter.ImportResult,
    ) {
        val protection = imported.protection
        if (protection != null) protections.remove(protection)
        if (imported.created) {
            if (protection != null) {
                runCatching { protection.abort() }
            } else if (target == null) {
                File(imported.path).delete()
            } else {
                runCatching { target.delete(File(imported.path).name) }
            }
            if (target != null) runCatching { externalSegments().remove(imported.path) }
        } else {
            runCatching { protection?.commit() }
        }
    }

    private fun normalizedPath(path: String): String = File(path).absolutePath

    private fun externalSegments(): ExternalSegmentRegistry {
        val current = externalSegmentRegistry
        if (current != null) return current
        return ExternalSegmentRegistry(directory, processName).also { externalSegmentRegistry = it }
    }

    internal class PreparedHandoff(
        internal val current: List<SegmentMigration>,
        internal val recovered: List<RecoveredSegmentMigration>,
    )

    internal class SegmentMigration(
        val index: Int,
        val source: CompletedSegment,
        val target: JankHunterBinaryStorage?,
        val imported: SealedSegmentImporter.ImportResult,
    )

    internal class RecoveredSegmentMigration(
        val sourcePath: String,
        val registeredExternal: Boolean,
        val imported: SealedSegmentImporter.ImportResult,
    )

    internal class CompletedSegment(
        var path: String,
        var storage: JankHunterBinaryStorage?,
        val allocation: SessionLogAllocator.Allocation,
        var protection: JankHunterBinaryArtifact?,
    )
}
