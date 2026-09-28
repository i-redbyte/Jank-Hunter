package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterLogSnapshot
import io.jankhunter.runtime.JankHunterSessionArchive
import io.jankhunter.runtime.internal.system.RetainedHeapDumper
import java.io.BufferedInputStream
import java.io.BufferedOutputStream
import java.io.File
import java.io.FileInputStream
import java.io.FilterInputStream
import java.io.FileOutputStream
import java.io.IOException
import java.io.InputStream
import java.io.RandomAccessFile
import java.util.zip.Deflater
import java.util.zip.ZipEntry
import java.util.zip.ZipFile
import java.util.zip.ZipOutputStream
import java.util.zip.ZipInputStream

/** Creates a bounded transport snapshot, coalescing configuration epochs from one process. */
internal object SessionArchiveExporter {
    fun export(
        root: File,
        destination: File,
        snapshot: JankHunterLogSnapshot?,
        includeHeapDumps: Boolean = true,
        maxBytesIncludingHeapDumps: Long = Long.MAX_VALUE,
    ): List<String> = exportInternal(root, destination, snapshot, includeHeapDumps, maxBytesIncludingHeapDumps, false)
        .map { it.archivePath }

    fun exportArtifacts(
        root: File,
        destination: File,
        snapshot: JankHunterLogSnapshot?,
        includeHeapDumps: Boolean = true,
        maxBytesIncludingHeapDumps: Long = Long.MAX_VALUE,
    ): List<JankHunterSessionArchive> =
        exportInternal(root, destination, snapshot, includeHeapDumps, maxBytesIncludingHeapDumps, true)

    private fun exportInternal(
        root: File,
        destination: File,
        snapshot: JankHunterLogSnapshot?,
        includeHeapDumps: Boolean,
        maxBytesIncludingHeapDumps: Long,
        verifyHeapDumps: Boolean,
    ): List<JankHunterSessionArchive> = SessionArtifactReadLeases.acquire(root).use {
        exportLeased(root, destination, snapshot, includeHeapDumps, maxBytesIncludingHeapDumps, verifyHeapDumps)
    }

    private fun exportLeased(
        root: File,
        destination: File,
        snapshot: JankHunterLogSnapshot?,
        includeHeapDumps: Boolean,
        maxBytesIncludingHeapDumps: Long,
        verifyHeapDumps: Boolean,
    ): List<JankHunterSessionArchive> {
        require(maxBytesIncludingHeapDumps >= 0L) { "Heap dump export budget must not be negative" }
        val rootCanonical = root.canonicalFile
        val destinationCanonical = destination.canonicalFile
        require(destinationCanonical != rootCanonical && !destinationCanonical.toPath().startsWith(rootCanonical.toPath())) {
            "Jank Hunter export destination must be outside the artifact root"
        }
        if (!destinationCanonical.isDirectory && !destinationCanonical.mkdirs()) {
            throw IOException("Cannot create Jank Hunter session export directory: $destinationCanonical")
        }
        require(destinationCanonical.listFiles().isNullOrEmpty()) {
            "Jank Hunter session export directory must be empty"
        }

        val current = snapshot?.let {
            require(it.logPaths.isNotEmpty()) { "Jank Hunter snapshot has no log files" }
            currentSessionPlan(rootCanonical, it.logPaths, includeHeapDumps, it.logByteLimits)
        }
        val historical = historicalArchives(rootCanonical)
        if (current != null) {
            require(historical.none { archive -> archive.name == current.archiveName }) {
                "Active Jank Hunter session already has a published archive"
            }
        }
        val excludedNames = historical.mapTo(HashSet(historical.size + 1)) { it.name }
        current?.let { excludedNames += it.archiveName }
        val unarchived = unarchivedSessionPlans(rootCanonical, excludedNames, includeHeapDumps)
        val units = ArrayList<ExportUnit>(historical.size + unarchived.size + if (current == null) 0 else 1)
        for (archive in historical) {
            units += ExportUnit(archive.name, archive, null, historicalProcessIds(archive))
        }
        for (session in unarchived) {
            units += ExportUnit(session.archiveName, null, session.sources, plannedProcessIds(session.sources))
        }
        current?.let { session ->
            units += ExportUnit(session.archiveName, null, session.sources, plannedProcessIds(session.sources))
        }
        units.sortBy(ExportUnit::archiveName)
        val groups = groupByProcess(units)
        val includeHeaps = includeHeapDumps && (maxBytesIncludingHeapDumps == Long.MAX_VALUE ||
            payloadFitsBudget(groups, units.lastOrNull(), maxBytesIncludingHeapDumps))
        var published = writeVerifiedArchives(destinationCanonical, groups, units.lastOrNull(), includeHeaps, verifyHeapDumps)
        // Metadata-only preflight avoids reading oversized dumps. ZIP headers are checked here.
        if (includeHeaps && !archivesFitBudget(published, maxBytesIncludingHeapDumps)) {
            for (archive in published) {
                if (!File(archive.archivePath).delete()) throw IOException("Cannot replace oversized Jank Hunter export: $archive")
            }
            published = writeArchives(destinationCanonical, groups, units.lastOrNull(), includeHeapDumps = false, verifyHeapDumps = verifyHeapDumps)
        }
        return published
    }

    private fun writeVerifiedArchives(
        destination: File,
        groups: Collection<List<ExportUnit>>,
        newest: ExportUnit?,
        includeHeapDumps: Boolean,
        verifyHeapDumps: Boolean,
    ): List<JankHunterSessionArchive> {
        val excluded = HashSet<HeapEntry>()
        while (true) {
            try {
                return writeArchives(destination, groups, newest, includeHeapDumps, verifyHeapDumps, excluded)
            } catch (error: IncompleteHeapDumpException) {
                // Failed temporary archives are removed by writeArchives. Retry only newly identified
                // bad entries, retaining complete dumps and the same captured JHLOG frontiers.
                if (!excluded.addAll(error.entries)) throw error
            }
        }
    }

    private fun writeArchives(
        destination: File,
        groups: Collection<List<ExportUnit>>,
        newest: ExportUnit?,
        includeHeapDumps: Boolean,
        verifyHeapDumps: Boolean,
        excluded: Set<HeapEntry> = emptySet(),
    ): List<JankHunterSessionArchive> {
        val published = ArrayList<JankHunterSessionArchive>(groups.size)
        try {
            for (group in groups) {
                val first = group.first()
                val keepHeapDumps = includeHeapDumps && group.any { it === newest }
                val archiveName = if (group.size == 1) first.archiveName else {
                    "${first.archiveName.removeSuffix(ARCHIVE_SUFFIX)}-epochs$ARCHIVE_SUFFIX"
                }
                val target = destination.resolve(archiveName)
                if (group.size > 1) {
                    try {
                        val heaps = writeMergedArchive(target, group, keepHeapDumps, verifyHeapDumps, excluded)
                        published += archiveMetadata(target, heaps)
                    } catch (_: DuplicateArchiveEntryException) {
                        for (unit in group) {
                            val fallback = destination.resolve(unit.archiveName)
                            val heaps = writeSingleArchive(fallback, unit, keepHeapDumps && unit === newest, verifyHeapDumps, excluded)
                            published += archiveMetadata(fallback, heaps)
                        }
                    }
                } else {
                    val heaps = writeSingleArchive(target, first, keepHeapDumps, verifyHeapDumps, excluded)
                    published += archiveMetadata(target, heaps)
                }
            }
            published.sortBy { it.archivePath }
            return published
        } catch (error: Throwable) {
            published.forEach { File(it.archivePath).delete() }
            destination.listFiles { file -> file.name.endsWith(TEMP_SUFFIX) }.orEmpty().forEach(File::delete)
            throw error
        }
    }

    private fun archivesFitBudget(archives: List<JankHunterSessionArchive>, budget: Long): Boolean {
        var remaining = budget
        for (archive in archives) {
            val size = archive.sizeBytes
            if (size > remaining) return false
            remaining -= size
        }
        return true
    }

    private fun payloadFitsBudget(
        groups: Collection<List<ExportUnit>>,
        newest: ExportUnit?,
        budget: Long,
    ): Boolean {
        var remaining = budget
        for (group in groups) {
            val keepHeapDumps = group.any { it === newest }
            for (unit in group) {
                val historical = unit.historical
                if (historical == null) {
                    for (source in checkNotNull(unit.sources)) {
                        if (!keepHeapDumps && isHeapSource(source)) continue
                        if (source.limit > remaining) return false
                        remaining -= source.limit
                    }
                } else {
                    ZipFile(historical).use { zip ->
                        val entries = zip.entries()
                        var count = 0
                        while (entries.hasMoreElements()) {
                            val entry = entries.nextElement()
                            require(++count <= MAX_CURRENT_ARTIFACTS) { "Jank Hunter archive entry count exceeds export limit" }
                            if (!keepHeapDumps && entry.name.endsWith(".hprof")) continue
                            // Unknown sizes cannot safely admit optional heap dumps.
                            if (entry.size < 0L || entry.size > remaining) return false
                            remaining -= entry.size
                        }
                    }
                }
            }
        }
        return true
    }

    private fun currentSessionPlan(
        root: File,
        paths: List<String>,
        includeHeapDumps: Boolean,
        limits: List<Long> = emptyList(),
    ): CurrentSessionPlan {
        require(paths.size <= MAX_CURRENT_ARTIFACTS) { "Jank Hunter snapshot file count exceeds export limit" }
        val uniquePaths = HashSet<String>(paths.size)
        val logs = ArrayList<SnapshotLog>(paths.size)
        var identity: SessionLogName.Parsed? = null
        var sessionDirectory: File? = null
        var hierarchical = true
        for ((index, path) in paths.withIndex()) {
            val source = File(path).canonicalFile
            require(uniquePaths.add(source.path)) { "Jank Hunter snapshot contains duplicate paths" }
            require(source.isFile && source.length() > 0L) { "Jank Hunter snapshot file is unavailable: ${source.name}" }
            val parsed = requireNotNull(SessionLogName.parse(source.name)) {
                "Jank Hunter snapshot contains a non-JHLOG file: ${source.name}"
            }
            val expected = identity
            if (expected == null) {
                identity = parsed
            } else {
                require(
                    expected.localDate == parsed.localDate &&
                        expected.runId == parsed.runId &&
                        expected.dailySessionIndex == parsed.dailySessionIndex
                ) { "Jank Hunter snapshot mixes multiple sessions" }
            }
            val process = source.parentFile?.canonicalFile
            val session = process?.parentFile?.canonicalFile
            val parsedSession = session?.let { candidate -> SessionArtifactPath.parseSessionDirectoryName(candidate.name) }
            val inHierarchy =
                process != null &&
                    session != null &&
                    session.parentFile == root &&
                    SessionArtifactPath.isCanonicalId(process.name) &&
                    parsedSession?.runId == parsed.runId &&
                    parsedSession.dailySessionIndex == parsed.dailySessionIndex
            if (!inHierarchy) {
                hierarchical = false
            } else if (sessionDirectory == null) {
                sessionDirectory = session
            } else {
                require(sessionDirectory == session) { "Jank Hunter snapshot mixes session directories" }
            }
            logs += SnapshotLog(source, process, limits.getOrNull(index)?.takeIf { it >= 0L } ?: source.length())
        }
        val resolvedIdentity = checkNotNull(identity)
        if (hierarchical) {
            addEarlierSessionSegments(checkNotNull(sessionDirectory), resolvedIdentity, logs, uniquePaths)
        }
        val archiveName = if (hierarchical) {
            "${checkNotNull(sessionDirectory).name}$ARCHIVE_SUFFIX"
        } else {
            "jh-session-${resolvedIdentity.localDate}-${resolvedIdentity.dailySessionIndex}-${resolvedIdentity.runId}$ARCHIVE_SUFFIX"
        }
        val sources = ArrayList<ArchiveSource>(logs.size + MAX_HEAP_DUMPS_PER_PROCESS * logs.size)
        val entryNames = HashSet<String>()
        val processDirectories = HashSet<File>()
        for (log in logs) {
            val entryName = if (hierarchical) "${checkNotNull(log.process).name}/${log.file.name}" else log.file.name
            require(entryNames.add(entryName)) { "Jank Hunter snapshot contains duplicate archive entries" }
            sources += ArchiveSource(log.file, entryName, log.limit)
            if (hierarchical) processDirectories += checkNotNull(log.process)
        }
        if (hierarchical && includeHeapDumps) {
            for (process in processDirectories) {
                val dumps = process.listFiles { file ->
                    file.isFile && file.length() > 0L && RetainedHeapDumper.isManagedHeapDumpFileName(file.name)
                }.orEmpty()
                require(dumps.size <= MAX_HEAP_DUMPS_PER_PROCESS) { "Jank Hunter heap dump count exceeds export limit" }
                for (dump in dumps) {
                    val canonicalDump = dump.canonicalFile
                    require(canonicalDump.parentFile == process && canonicalDump.name == dump.name) {
                        "Jank Hunter heap dump escaped its process directory"
                    }
                    val entryName = "${process.name}/${dump.name}"
                    require(entryNames.add(entryName)) { "Jank Hunter snapshot contains duplicate archive entries" }
                    sources += ArchiveSource(canonicalDump, entryName)
                }
            }
        }
        sources.sortBy(ArchiveSource::entryName)
        return CurrentSessionPlan(archiveName, sources)
    }

    private fun addEarlierSessionSegments(
        session: File,
        identity: SessionLogName.Parsed,
        logs: MutableList<SnapshotLog>,
        knownPaths: MutableSet<String>,
    ) {
        val snapshotFrontiers = HashMap<File, Long>()
        for (log in logs) {
            val process = checkNotNull(log.process)
            val segment = checkNotNull(SessionLogName.parse(log.file.name)).segmentIndex
            val previous = snapshotFrontiers[process]
            if (previous == null || segment > previous) snapshotFrontiers[process] = segment
        }
        val processes = session.listFiles().orEmpty()
        require(processes.size <= MAX_CURRENT_ARTIFACTS) { "Jank Hunter process count exceeds export limit" }
        for (process in processes) {
            if (!process.isDirectory || !SessionArtifactPath.isCanonicalId(process.name)) continue
            val canonicalProcess = process.canonicalFile
            if (canonicalProcess.parentFile != session) continue
            val frontier = snapshotFrontiers[canonicalProcess]
            val files = canonicalProcess.listFiles().orEmpty()
            require(files.size <= MAX_CURRENT_ARTIFACTS) { "Jank Hunter process file count exceeds export limit" }
            for (file in files) {
                val parsed = SessionLogName.parse(file.name) ?: continue
                if (parsed.localDate != identity.localDate || parsed.runId != identity.runId ||
                    parsed.dailySessionIndex != identity.dailySessionIndex ||
                    (frontier != null && parsed.segmentIndex >= frontier) ||
                    !file.isFile || file.length() <= 0L
                ) continue
                val canonicalFile = file.canonicalFile
                if (canonicalFile.parentFile != canonicalProcess || !knownPaths.add(canonicalFile.path)) continue
                require(logs.size < MAX_CURRENT_ARTIFACTS) { "Jank Hunter session file count exceeds export limit" }
                logs += SnapshotLog(canonicalFile, canonicalProcess)
            }
        }
    }

    private fun historicalArchives(root: File): List<File> {
        if (!root.isDirectory) return emptyList()
        val files = root.listFiles { file ->
            file.isFile &&
                file.length() > 0L &&
                file.name.endsWith(ARCHIVE_SUFFIX) &&
                SessionArtifactPath.parseSessionDirectoryName(file.name.removeSuffix(ARCHIVE_SUFFIX)) != null
        }.orEmpty()
        require(files.size <= MAX_HISTORICAL_ARCHIVES) { "Jank Hunter historical archive count exceeds export limit" }
        require(files.all { it.canonicalFile.parentFile == root && it.canonicalFile.name == it.name }) {
            "Jank Hunter historical archive escaped artifact root"
        }
        return files.sortedBy(File::getName)
    }

    private fun unarchivedSessionPlans(
        root: File,
        excludedNames: Set<String>,
        includeHeapDumps: Boolean,
    ): List<CurrentSessionPlan> {
        if (!root.isDirectory) return emptyList()
        val sessions = root.listFiles { file ->
            file.isDirectory && SessionArtifactPath.parseSessionDirectoryName(file.name) != null
        }.orEmpty()
        require(sessions.size <= MAX_HISTORICAL_ARCHIVES) { "Jank Hunter session directory count exceeds export limit" }
        val plans = ArrayList<CurrentSessionPlan>(sessions.size)
        for (session in sessions) {
            if ("${session.name}$ARCHIVE_SUFFIX" in excludedNames) continue
            require(session.canonicalFile.parentFile == root) { "Jank Hunter session directory escaped artifact root" }
            val processes = session.listFiles().orEmpty()
            require(processes.size <= MAX_CURRENT_ARTIFACTS) { "Jank Hunter process count exceeds export limit" }
            val paths = ArrayList<String>()
            for (process in processes) {
                require(process.isDirectory && SessionArtifactPath.isCanonicalId(process.name)) {
                    "Invalid Jank Hunter process directory: ${process.name}"
                }
                require(process.canonicalFile.parentFile == session.canonicalFile) {
                    "Jank Hunter process directory escaped its session"
                }
                val artifacts = process.listFiles().orEmpty()
                require(artifacts.size <= MAX_CURRENT_ARTIFACTS - paths.size) {
                    "Jank Hunter session file count exceeds export limit"
                }
                for (artifact in artifacts) {
                    if (SessionLogName.parse(artifact.name) != null) paths += artifact.path
                }
            }
            if (paths.isEmpty()) continue
            val plan = currentSessionPlan(root, paths, includeHeapDumps)
            require(plan.archiveName == "${session.name}$ARCHIVE_SUFFIX") {
                "Jank Hunter session artifact identity does not match directory"
            }
            plans += plan
        }
        return plans.sortedBy(CurrentSessionPlan::archiveName)
    }

    private fun historicalProcessIds(archive: File): Set<String> {
        val identity = requireNotNull(SessionArtifactPath.parseSessionDirectoryName(archive.name.removeSuffix(ARCHIVE_SUFFIX))) {
            "Invalid Jank Hunter historical archive name"
        }
        return ZipFile(archive).use { zip ->
            val processes = HashSet<String>()
            val names = HashSet<String>()
            val entries = zip.entries()
            var count = 0
            var logs = 0
            while (entries.hasMoreElements()) {
                val entry = entries.nextElement()
                require(++count <= MAX_CURRENT_ARTIFACTS) { "Jank Hunter archive entry count exceeds export limit" }
                require(!entry.isDirectory && (entry.method == ZipEntry.STORED || entry.method == ZipEntry.DEFLATED) &&
                    entry.size >= 0L && names.add(entry.name)
                ) { "Invalid Jank Hunter historical archive entry" }
                val separator = entry.name.indexOf('/')
                require(separator > 0 && separator == entry.name.lastIndexOf('/') && separator < entry.name.lastIndex) {
                    "Unsafe Jank Hunter historical archive entry name"
                }
                val process = entry.name.substring(0, separator)
                val name = entry.name.substring(separator + 1)
                require(SessionArtifactPath.isCanonicalId(process)) { "Invalid Jank Hunter historical process ID" }
                val parsed = SessionLogName.parse(name)
                if (parsed != null) {
                    require(parsed.runId == identity.runId && parsed.dailySessionIndex == identity.dailySessionIndex) {
                        "Jank Hunter historical JHLOG belongs to another session"
                    }
                    processes += process
                    logs++
                } else {
                    require(RetainedHeapDumper.isManagedHeapDumpFileName(name)) {
                        "Unknown Jank Hunter historical archive artifact"
                    }
                }
            }
            require(logs > 0) { "Jank Hunter historical archive has no JHLOG entries" }
            processes
        }
    }

    private fun plannedProcessIds(sources: List<ArchiveSource>): Set<String> {
        val processes = HashSet<String>()
        for (source in sources) {
            val process = source.entryName.substringBefore('/', "")
            val name = source.entryName.substringAfter('/', "")
            if (SessionArtifactPath.isCanonicalId(process) && SessionLogName.parse(name) != null) {
                processes += process
            }
        }
        return processes
    }

    private fun groupByProcess(units: List<ExportUnit>): Collection<List<ExportUnit>> {
        val parents = IntArray(units.size) { it }
        val sizes = IntArray(units.size) { 1 }
        val firstByProcess = HashMap<String, Int>()
        for (index in units.indices) {
            for (process in units[index].processIds) {
                val first = firstByProcess.put(process, index) ?: continue
                var left = findParent(parents, index)
                var right = findParent(parents, first)
                if (left == right) continue
                if (sizes[left] < sizes[right]) {
                    val swap = left
                    left = right
                    right = swap
                }
                parents[right] = left
                sizes[left] += sizes[right]
            }
        }
        val grouped = LinkedHashMap<Int, MutableList<ExportUnit>>()
        for (index in units.indices) {
            grouped.getOrPut(findParent(parents, index)) { ArrayList() } += units[index]
        }
        return grouped.values
    }

    private fun findParent(parents: IntArray, index: Int): Int {
        var current = index
        while (parents[current] != current) {
            parents[current] = parents[parents[current]]
            current = parents[current]
        }
        return current
    }

    private fun isHeapSource(source: ArchiveSource): Boolean =
        RetainedHeapDumper.isManagedHeapDumpFileName(source.file.name)

    private fun writeSingleArchive(
        target: File, unit: ExportUnit, includeHeapDumps: Boolean, verifyHeapDumps: Boolean, excluded: Set<HeapEntry>,
    ): Int {
        val historical = unit.historical
        if (historical != null) {
            if (includeHeapDumps) {
                if (excluded.any { it.archive == unit.archiveName }) {
                    return writeMergedArchive(target, listOf(unit), true, verifyHeapDumps, excluded)
                }
                return copyAtomically(historical, target, verifyHeapDumps)
            }
            copyJhlogOnlyArchive(historical, target)
            return 0
        } else {
            val sources = checkNotNull(unit.sources)
            return writeCurrentArchive(target, if (includeHeapDumps) sources else sources.filterNot { isHeapSource(it) }, verifyHeapDumps, excluded)
        }
    }

    private fun writeMergedArchive(
        target: File, units: List<ExportUnit>, includeHeapDumps: Boolean, verifyHeapDumps: Boolean, excluded: Set<HeapEntry>,
    ): Int {
        val heaps = HeapCopyState(verifyHeapDumps, excluded)
        var completedHeaps = 0
        require(!target.exists()) { "Jank Hunter export destination already exists: $target" }
        val temporary = checkNotNull(target.parentFile).resolve(".${target.name}$TEMP_SUFFIX")
        var published = false
        try {
            FileOutputStream(temporary).use { rawOutput ->
                ZipOutputStream(BufferedOutputStream(rawOutput, COPY_BUFFER_BYTES)).use { output ->
                    output.setLevel(Deflater.NO_COMPRESSION)
                    val buffer = ByteArray(COPY_BUFFER_BYTES)
                    val names = HashSet<String>()
                    for (unit in units) {
                        val historical = unit.historical
                        if (historical != null) {
                            ZipFile(historical).use { zip ->
                                val entries = zip.entries()
                                var count = 0
                                while (entries.hasMoreElements()) {
                                    val entry = entries.nextElement()
                                    require(++count <= MAX_CURRENT_ARTIFACTS) {
                                        "Jank Hunter archive entry count exceeds export limit"
                                    }
                                    val process = entry.name.substringBefore('/', "")
                                    val name = entry.name.substringAfter('/', "")
                                    if (!includeHeapDumps &&
                                        (!SessionArtifactPath.isCanonicalId(process) || SessionLogName.parse(name) == null)
                                    ) continue
                                    zip.getInputStream(entry).use { input ->
                                        completedHeaps += copyMergedEntry(output, HeapEntry(unit.archiveName, entry.name), input, buffer, names, heaps)
                                    }
                                }
                            }
                        } else {
                            for (source in checkNotNull(unit.sources)) {
                                if (!includeHeapDumps && isHeapSource(source)) continue
                                ArtifactFrontierInputStream(source.file, source.limit).use { input ->
                                    completedHeaps += copyMergedEntry(output, HeapEntry(unit.archiveName, source.entryName), input, buffer, names, heaps)
                                }
                            }
                        }
                    }
                }
            }
            heaps.requireComplete()
            RandomAccessFile(temporary, "rw").use { file ->
                if (file.length() <= 0L) throw IOException("Jank Hunter merged session archive is empty")
                file.fd.sync()
            }
            if (!temporary.renameTo(target)) throw IOException("Cannot publish Jank Hunter merged archive: $target")
            published = true
            return completedHeaps
        } finally {
            if (!published) temporary.delete()
        }
    }

    private fun copyMergedEntry(
        output: ZipOutputStream,
        entry: HeapEntry,
        input: InputStream,
        buffer: ByteArray,
        names: MutableSet<String>,
        heaps: HeapCopyState,
    ): Int {
        if (entry in heaps.excluded) return 0
        val name = entry.name
        if (!names.add(name)) throw DuplicateArchiveEntryException(name)
        output.putNextEntry(ZipEntry(name).apply { time = 0L })
        val validator = heapValidator(name)
        while (true) {
            val count = input.read(buffer)
            if (count < 0) break
            if (count > 0) {
                validator?.accept(buffer, 0, count)
                output.write(buffer, 0, count)
            }
        }
        output.closeEntry()
        heaps.record(entry, validator)
        return if (validator?.isComplete() == true) 1 else 0
    }

    private fun copyJhlogOnlyArchive(source: File, target: File) {
        require(!target.exists()) { "Jank Hunter export destination already exists: $target" }
        val temporary = checkNotNull(target.parentFile).resolve(".${target.name}$TEMP_SUFFIX")
        var published = false
        try {
            ZipFile(source).use { input ->
                FileOutputStream(temporary).use { rawOutput ->
                    ZipOutputStream(BufferedOutputStream(rawOutput, COPY_BUFFER_BYTES)).use { output ->
                        output.setLevel(Deflater.NO_COMPRESSION)
                        val buffer = ByteArray(COPY_BUFFER_BYTES)
                        var count = 0
                        var logCount = 0
                        val entries = input.entries()
                        while (entries.hasMoreElements()) {
                            val entry = entries.nextElement()
                            require(++count <= MAX_CURRENT_ARTIFACTS) { "Jank Hunter archive entry count exceeds export limit" }
                            val process = entry.name.substringBefore('/', "")
                            val name = entry.name.substringAfter('/', "")
                            if (!SessionArtifactPath.isCanonicalId(process) || SessionLogName.parse(name) == null) continue
                            logCount++
                            output.putNextEntry(ZipEntry(entry.name).apply { time = 0L })
                            input.getInputStream(entry).use { stream ->
                                while (true) {
                                    val read = stream.read(buffer)
                                    if (read < 0) break
                                    if (read > 0) output.write(buffer, 0, read)
                                }
                            }
                            output.closeEntry()
                        }
                        require(logCount > 0) { "Jank Hunter historical archive has no JHLOG entries" }
                    }
                }
            }
            RandomAccessFile(temporary, "rw").use { it.fd.sync() }
            if (!temporary.renameTo(target)) throw IOException("Cannot publish Jank Hunter exported archive: $target")
            published = true
        } finally {
            if (!published) temporary.delete()
        }
    }

    private fun copyAtomically(source: File, target: File, verifyHeapDumps: Boolean): Int {
        // Validate the central directory before a streaming tee; ZipInputStream alone accepts a
        // truncated archive with no end record. Opening ZipFile reads metadata, not heap payloads.
        val expectedNames = ZipFile(source).use { zip ->
            require(zip.size() > 0) { "Empty session archive" }
            zip.entries().asSequence().mapTo(HashSet(zip.size())) { it.name }
        }
        var completedHeaps = 0
        require(!target.exists()) { "Jank Hunter export destination already exists: $target" }
        val temporary = checkNotNull(target.parentFile).resolve(".${target.name}$TEMP_SUFFIX")
        var published = false
        try {
            FileInputStream(source).use { rawInput ->
                BufferedInputStream(rawInput, COPY_BUFFER_BYTES).use { input ->
                    FileOutputStream(temporary).use { rawOutput ->
                        BufferedOutputStream(rawOutput, COPY_BUFFER_BYTES).use { output ->
                            completedHeaps = copyAndInspectArchive(
                                source.name, input, output, expectedNames, verifyHeapDumps,
                            )
                        }
                    }
                }
            }
            RandomAccessFile(temporary, "rw").use { file -> file.fd.sync() }
            if (temporary.length() != source.length()) throw IOException("Jank Hunter archive changed during export: $source")
            if (!temporary.renameTo(target)) throw IOException("Cannot publish Jank Hunter exported archive: $target")
            published = true
            return completedHeaps
        } finally {
            if (!published) temporary.delete()
        }
    }

    private fun writeCurrentArchive(
        target: File, sources: List<ArchiveSource>, verifyHeapDumps: Boolean, excluded: Set<HeapEntry>,
    ): Int {
        val heaps = HeapCopyState(verifyHeapDumps, excluded)
        var completedHeaps = 0
        require(!target.exists()) { "Jank Hunter export destination already exists: $target" }
        val temporary = checkNotNull(target.parentFile).resolve(".${target.name}$TEMP_SUFFIX")
        var published = false
        try {
            FileOutputStream(temporary).use { rawOutput ->
                ZipOutputStream(BufferedOutputStream(rawOutput, COPY_BUFFER_BYTES)).use { output ->
                    output.setLevel(Deflater.NO_COMPRESSION)
                    val buffer = ByteArray(COPY_BUFFER_BYTES)
                    val names = HashSet<String>()
                    for (source in sources) {
                        ArtifactFrontierInputStream(source.file, source.limit).use { input ->
                            completedHeaps += copyMergedEntry(output, HeapEntry(target.name, source.entryName), input, buffer, names, heaps)
                        }
                    }
                }
            }
            heaps.requireComplete()
            RandomAccessFile(temporary, "rw").use { file ->
                if (file.length() <= 0L) throw IOException("Jank Hunter current session archive is empty")
                file.fd.sync()
            }
            if (!temporary.renameTo(target)) throw IOException("Cannot publish Jank Hunter current session archive: $target")
            published = true
            return completedHeaps
        } finally {
            if (!published) temporary.delete()
        }
    }

    private fun archiveMetadata(file: File, completedHeaps: Int) =
        JankHunterSessionArchive(file.absolutePath, file.length(), completedHeaps)

    private fun heapValidator(name: String): HeapDumpCompletionValidator? =
        if (name.endsWith(".hprof")) HeapDumpCompletionValidator() else null

    /** Tee compressed bytes once; inspecting entries neither rereads nor recompresses the archive. */
    private fun copyAndInspectArchive(
        archive: String,
        input: InputStream,
        output: java.io.OutputStream,
        expectedNames: MutableSet<String>,
        verifyHeapDumps: Boolean,
    ): Int {
        val heaps = HeapCopyState(verify = verifyHeapDumps, excluded = emptySet())
        val tee = object : FilterInputStream(input) {
            override fun read(): Int = input.read().also { if (it >= 0) output.write(it) }
            override fun read(bytes: ByteArray, offset: Int, length: Int): Int =
                input.read(bytes, offset, length).also { if (it > 0) output.write(bytes, offset, it) }
        }
        val buffer = ByteArray(COPY_BUFFER_BYTES)
        var completedHeaps = 0
        ZipInputStream(tee).use { zip ->
            var entries = 0
            while (true) {
                val entry = zip.nextEntry ?: break
                require(++entries <= MAX_CURRENT_ARTIFACTS) { "Jank Hunter archive entry count exceeds export limit" }
                require(expectedNames.remove(entry.name)) { "Jank Hunter archive local entry differs from central directory" }
                val validator = if (verifyHeapDumps) heapValidator(entry.name) else null
                while (true) {
                    val count = zip.read(buffer)
                    if (count < 0) break
                    if (count > 0) validator?.accept(buffer, 0, count)
                }
                zip.closeEntry()
                heaps.record(HeapEntry(archive, entry.name), validator)
                if (validator?.isComplete() == true) completedHeaps++
            }
            // ZipInputStream stops before the central directory; the tee has already copied read-ahead.
            while (tee.read(buffer) >= 0) { /* Preserve remaining central-directory bytes. */ }
        }
        require(expectedNames.isEmpty()) { "Jank Hunter archive is missing a central-directory entry" }
        heaps.requireComplete()
        return completedHeaps
    }

    private data class SnapshotLog(val file: File, val process: File?, val limit: Long = file.length())
    private data class ArchiveSource(val file: File, val entryName: String, val limit: Long = file.length())
    private data class CurrentSessionPlan(val archiveName: String, val sources: List<ArchiveSource>)
    private data class ExportUnit(
        val archiveName: String,
        val historical: File?,
        val sources: List<ArchiveSource>?,
        val processIds: Set<String>,
    )
    private data class HeapEntry(val archive: String, val name: String)

    private class HeapCopyState(private val verify: Boolean, val excluded: Set<HeapEntry>) {
        private val incomplete = HashSet<HeapEntry>()
        fun record(entry: HeapEntry, validator: HeapDumpCompletionValidator?) {
            if (verify && validator != null && !validator.isComplete()) incomplete += entry
        }
        fun requireComplete() {
            if (incomplete.isNotEmpty()) throw IncompleteHeapDumpException(incomplete)
        }
    }

    private class IncompleteHeapDumpException(val entries: Set<HeapEntry>) :
        IOException("Incomplete HPROF excluded from session export")

    private class DuplicateArchiveEntryException(name: String) : IOException("Duplicate session archive entry: $name")

    private const val ARCHIVE_SUFFIX = ".jhlog.zip"
    private const val TEMP_SUFFIX = ".tmp"
    private const val COPY_BUFFER_BYTES = 64 * 1024
    private const val MAX_CURRENT_ARTIFACTS = 4_096
    private const val MAX_HISTORICAL_ARCHIVES = 1_024
    private const val MAX_HEAP_DUMPS_PER_PROCESS = 64
}
