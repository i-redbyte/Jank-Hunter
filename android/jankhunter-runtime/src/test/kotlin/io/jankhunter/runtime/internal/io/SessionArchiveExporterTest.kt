package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterLogSnapshot
import java.io.File
import java.nio.file.Files
import java.util.zip.ZipEntry
import java.util.zip.ZipFile
import java.util.zip.ZipOutputStream
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class SessionArchiveExporterTest {
    @Test
    fun historicalArchiveWithTraversalEntryIsRejectedBeforeRawCopy() {
        val root = tempDir("traversal-root")
        val destination = tempDir("traversal-export")
        try {
            val source = root.resolve("2026-09-24T10-00-00.000Z_2_$CURRENT_RUN_ID.jhlog.zip")
            ZipOutputStream(source.outputStream()).use { zip ->
                zip.putNextEntry(ZipEntry("$PROCESS_A/../../outside.txt"))
                zip.write(byteArrayOf(1, 2, 3))
                zip.closeEntry()
            }
            assertThrows(IllegalArgumentException::class.java) {
                SessionArchiveExporter.export(root, destination, null)
            }
            assertTrue(destination.listFiles().isNullOrEmpty())
        } finally {
            root.deleteRecursively()
            destination.deleteRecursively()
        }
    }

    @Test
    fun managedHeapSymlinkOutsideTheSessionIsNeverExported() {
        withBudgetFixture { root, destination, _, heap ->
            val outside = tempDir("outside-heap").resolve("private.bin")
            try {
                outside.writeBytes(completedHeapBytes())
                assertTrue(heap.delete())
                Files.createSymbolicLink(heap.toPath(), outside.toPath())

                assertThrows(IllegalArgumentException::class.java) {
                    SessionArchiveExporter.export(root, destination, null)
                }
                assertTrue(destination.listFiles().isNullOrEmpty())
            } finally {
                outside.parentFile?.deleteRecursively()
            }
        }
    }

    @Test
    fun metadataReflectsOnlyCompletedIncludedDumpsOnEveryExport() {
        withBudgetFixture { root, destination, log, heap ->
            heap.writeBytes(completedHeapBytes())
            val first = SessionArchiveExporter.exportArtifacts(root, destination, null).single()
            assertTrue(first.containsCompletedHeapDump)
            assertEquals(1, first.completedHeapDumpCount)
            assertEquals(File(first.archivePath).length(), first.sizeBytes)
            File(first.archivePath).delete()
            val second = SessionArchiveExporter.exportArtifacts(root, destination, null).single()
            assertTrue(second.containsCompletedHeapDump)
            assertLogBytes(File(second.archivePath), log)
            File(second.archivePath).delete()
            val omitted = SessionArchiveExporter.exportArtifacts(root, destination, null,
                maxBytesIncludingHeapDumps = heap.length() + log.length()).single()
            assertFalse(omitted.containsCompletedHeapDump)
            assertEquals(0, omitted.completedHeapDumpCount)
        }
    }

    @Test
    fun metadataRejectsRandomAndPartialHeapPayloadsWithoutLosingLogs() {
        withBudgetFixture { root, destination, log, heap ->
            for (bytes in listOf(ByteArray(4096), byteArrayOf(), "hprof".toByteArray(),
                completedHeapBytes().dropLast(9).toByteArray())) {
                heap.writeBytes(bytes)
                val result = SessionArchiveExporter.exportArtifacts(root, destination, null).single()
                assertFalse(result.containsCompletedHeapDump)
                assertLogBytes(File(result.archivePath), log)
                assertFalse(archiveEntries(File(result.archivePath)).any { it.endsWith(".hprof") })
                File(result.archivePath).delete()
            }
        }
    }

    @Test
    fun mixedCurrentHeapsKeepCompletedPayloadWhileExcludingOnlyInvalidEntries() {
        assertMixedHeapExport(historical = false, merged = false)
    }

    @Test
    fun mixedHistoricalHeapsRewriteArchiveWithoutDiscardingCompletedPayload() {
        assertMixedHeapExport(historical = true, merged = false)
    }

    @Test
    fun mixedMergedHeapsKeepCompletedPayloadAndEveryLog() {
        assertMixedHeapExport(historical = false, merged = true)
    }

    private fun assertMixedHeapExport(historical: Boolean, merged: Boolean) {
        withBudgetFixture { root, destination, log, heap ->
            val expected = completedHeapBytes()
            heap.writeBytes(expected)
            val invalid = checkNotNull(heap.parentFile).resolve("retained-1727172000001-Partial-2.hprof")
                .apply { writeBytes(expected.dropLast(9).toByteArray()) }
            val random = checkNotNull(heap.parentFile).resolve("retained-1727172000002-Random-3.hprof")
                .apply { writeText("not a heap") }
            if (merged) sessionLog(root, "2026-09-24T09-00-00.000Z_1_$HISTORICAL_RUN_ID", PROCESS_A, 1L)
            if (historical) {
                val initial = File(SessionArchiveExporter.export(root, destination, null).single())
                initial.copyTo(root.resolve(initial.name))
                checkNotNull(log.parentFile?.parentFile).deleteRecursively()
                initial.delete()
            }
            val result = SessionArchiveExporter.exportArtifacts(root, destination, null).single()
            assertEquals(1, result.completedHeapDumpCount)
            assertTrue(result.containsCompletedHeapDump)
            ZipFile(result.archivePath).use { zip ->
                assertArrayEquals(expected, zip.getInputStream(zip.getEntry("$PROCESS_A/${heap.name}")).use { it.readBytes() })
                assertEquals(null, zip.getEntry("$PROCESS_A/${invalid.name}"))
                assertEquals(null, zip.getEntry("$PROCESS_A/${random.name}"))
                assertTrue(zip.getEntry("$PROCESS_A/${log.name}") != null)
            }
            if (!historical) {
                assertTrue(invalid.isFile)
                assertTrue(random.isFile)
            }
            assertFalse(destination.listFiles().orEmpty().any { it.name.endsWith(".tmp") })
        }
    }

    @Test
    fun verifiedExportRejectsMalformedHistoricalZip() {
        val root = tempDir("broken-zip-root")
        val destination = tempDir("broken-zip-export")
        try {
            root.resolve("2026-09-24T10-00-00.000Z_2_$CURRENT_RUN_ID.jhlog.zip")
                .writeBytes(byteArrayOf(11, 12, 13))
            assertThrows(java.io.IOException::class.java) {
                SessionArchiveExporter.exportArtifacts(root, destination, null)
            }
            assertTrue(destination.listFiles().isNullOrEmpty())
        } finally {
            root.deleteRecursively()
            destination.deleteRecursively()
        }
    }

    @Test
    fun historicalMetadataInspectsStreamWhilePreservingCompressedArchiveBytes() {
        withBudgetFixture { root, destination, log, heap ->
            heap.writeBytes(completedHeapBytes())
            val initial = File(SessionArchiveExporter.export(root, destination, null).single())
            val bytes = initial.readBytes()
            initial.copyTo(root.resolve(initial.name))
            checkNotNull(log.parentFile?.parentFile).deleteRecursively()
            initial.delete()
            val result = SessionArchiveExporter.exportArtifacts(root, destination, null).single()
            assertTrue(result.containsCompletedHeapDump)
            assertArrayEquals(bytes, File(result.archivePath).readBytes())
        }
    }

    @Test
    fun metadataCountsEachCompletedHeapInMergedArchive() {
        withBudgetFixture { root, destination, log, heap ->
            heap.writeBytes(completedHeapBytes())
            checkNotNull(heap.parentFile).resolve("retained-1727172000001-OtherActivity-2.hprof").writeBytes(completedHeapBytes())
            sessionLog(root, "2026-09-24T09-00-00.000Z_1_$HISTORICAL_RUN_ID", PROCESS_A, 1L)
            val result = SessionArchiveExporter.exportArtifacts(root, destination, null).single()
            assertEquals(2, result.completedHeapDumpCount)
            assertTrue(result.archivePath.contains("-epochs"))
            assertLogBytes(File(result.archivePath), log)
        }
    }

    @Test
    fun historicalInspectionPreservesLargeCentralDirectory() {
        val root = tempDir("central-root")
        val destination = tempDir("central-export")
        try {
            val source = root.resolve("2026-09-24T10-00-00.000Z_2_$CURRENT_RUN_ID.jhlog.zip")
            ZipOutputStream(source.outputStream()).use { zip ->
                repeat(1_000) { index ->
                    val suffix = if (index == 0) "" else "-$index"
                    zip.putNextEntry(ZipEntry("$PROCESS_A/jh-session-log.2026-09-24.$CURRENT_RUN_ID.2$suffix.jhlog"))
                    zip.write(byteArrayOf(1, 2, 3))
                    zip.closeEntry()
                }
                zip.putNextEntry(ZipEntry("$PROCESS_A/retained-1727172000000-Test-1.hprof"))
                zip.write(completedHeapBytes())
                zip.closeEntry()
            }
            val result = SessionArchiveExporter.exportArtifacts(root, destination, null).single()
            assertTrue(result.containsCompletedHeapDump)
            assertArrayEquals(source.readBytes(), File(result.archivePath).readBytes())
        } finally {
            root.deleteRecursively()
            destination.deleteRecursively()
        }
    }

    private fun completedHeapBytes(): ByteArray {
        val bytes = java.io.ByteArrayOutputStream()
        java.io.DataOutputStream(bytes).use { output ->
            output.write("JAVA PROFILE 1.0.3\u0000".toByteArray())
            output.writeInt(4)
            output.writeLong(0)
            output.writeByte(0x1c)
            output.writeInt(0)
            output.writeInt(5)
            output.writeByte(0xff)
            output.writeInt(1)
            output.writeByte(0x2c)
            output.writeInt(0)
            output.writeInt(0)
        }
        return bytes.toByteArray()
    }

    @Test
    fun oversizedHeapIsOmittedWithoutChangingLogsOrSourceDump() {
        withBudgetFixture { root, destination, log, heap ->
            val exported = SessionArchiveExporter.export(root, destination, null, maxBytesIncludingHeapDumps = 1024L)
            assertEquals(setOf("$PROCESS_A/${log.name}"), archiveEntries(File(exported.single())))
            assertLogBytes(File(exported.single()), log)
            assertEquals(4096L, heap.length())
            assertEquals(1, destination.listFiles().orEmpty().size)
        }
    }

    @Test
    fun affordableHeapRemainsInsideTheOnlySessionArchive() {
        withBudgetFixture { root, destination, log, heap ->
            val exported = SessionArchiveExporter.export(root, destination, null, maxBytesIncludingHeapDumps = 8192L)
            assertEquals(setOf("$PROCESS_A/${log.name}", "$PROCESS_A/${heap.name}"), archiveEntries(File(exported.single())))
            ZipFile(exported.single()).use { zip ->
                assertArrayEquals(heap.readBytes(), zip.getInputStream(zip.getEntry("$PROCESS_A/${heap.name}")).use { it.readBytes() })
            }
            assertEquals(1, destination.listFiles().orEmpty().size)
        }
    }

    @Test
    fun zipOverheadTriggersHeapFallbackAtTheSameSnapshotFrontier() {
        withBudgetFixture { root, destination, log, heap ->
            val snapshot = JankHunterLogSnapshot(42L, listOf(log.path), processCount = 1, logByteLimits = listOf(1L))
            val budget = heap.length() + 1L
            val exported = SessionArchiveExporter.export(root, destination, snapshot, maxBytesIncludingHeapDumps = budget)
            assertEquals(setOf("$PROCESS_A/${log.name}"), archiveEntries(File(exported.single())))
            ZipFile(exported.single()).use { zip ->
                assertArrayEquals(byteArrayOf(log.readBytes().first()), zip.getInputStream(zip.getEntry("$PROCESS_A/${log.name}")).use { it.readBytes() })
            }
            assertTrue(File(exported.single()).length() < budget)
            assertEquals(4096L, heap.length())
        }
    }

    @Test
    fun budgetNeverTruncatesJhlogEvenWhenLogsAloneExceedIt() {
        withBudgetFixture { root, destination, log, heap ->
            val exported = SessionArchiveExporter.export(root, destination, null, maxBytesIncludingHeapDumps = 0L)
            assertEquals(setOf("$PROCESS_A/${log.name}"), archiveEntries(File(exported.single())))
            assertLogBytes(File(exported.single()), log)
            assertTrue(heap.isFile)
        }
    }

    @Test
    fun publishedHeapIsOmittedOnlyFromTransportCopy() {
        withBudgetFixture { root, destination, log, heap ->
            val historical = File(SessionArchiveExporter.export(root, destination, null).single())
            val originalBytes = historical.readBytes()
            val source = root.resolve(historical.name)
            historical.copyTo(source)
            checkNotNull(log.parentFile?.parentFile).deleteRecursively()
            destination.deleteRecursively()
            destination.mkdirs()
            val exported = SessionArchiveExporter.export(root, destination, null, maxBytesIncludingHeapDumps = 1024L)
            assertEquals(setOf("$PROCESS_A/${log.name}"), archiveEntries(File(exported.single())))
            assertArrayEquals(originalBytes, source.readBytes())
            assertTrue(archiveEntries(source).contains("$PROCESS_A/${heap.name}"))
        }
    }

    @Test
    fun heapBudgetIncludesEveryExportedSession() {
        withBudgetFixture { root, destination, log, _ ->
            sessionLog(root, "2026-09-24T09-00-00.000Z_1_$HISTORICAL_RUN_ID", PROCESS_B, 1L).writeBytes(ByteArray(4096))
            val exported = SessionArchiveExporter.export(root, destination, null, maxBytesIncludingHeapDumps = 8192L)
            assertEquals(2, exported.size)
            assertTrue(exported.all { path -> archiveEntries(File(path)).all { it.endsWith(".jhlog") } })
            assertLogBytes(File(exported.last()), log)
        }
    }

    private fun withBudgetFixture(block: (File, File, File, File) -> Unit) {
        val root = tempDir("heap-budget-root")
        val destination = tempDir("heap-budget-export")
        try {
            val log = sessionLog(root, "2026-09-24T10-00-00.000Z_2_$CURRENT_RUN_ID", PROCESS_A, 2L)
            log.writeBytes(byteArrayOf(1, 2, 3))
            val heap = checkNotNull(log.parentFile).resolve("retained-1727172000000-LeakedActivity-1.hprof")
            heap.writeBytes(ByteArray(4096))
            block(root, destination, log, heap)
        } finally {
            root.deleteRecursively()
            destination.deleteRecursively()
        }
    }

    private fun assertLogBytes(archive: File, log: File) {
        ZipFile(archive).use { zip ->
            assertArrayEquals(log.readBytes(), zip.getInputStream(zip.getEntry("$PROCESS_A/${log.name}")).use { it.readBytes() })
        }
    }

    @Test
    fun groupsConfigurationEpochsFromOneProcessIntoOnePortableArchive() {
        val root = tempDir("same-process-epochs")
        val destination = tempDir("same-process-export")
        try {
            val first = sessionLog(root, "2026-09-24T10-00-00.000Z_2_$CURRENT_RUN_ID", PROCESS_A, 2L)
            val second = sessionLog(root, "2026-09-24T11-00-00.000Z_3_$OTHER_RUN_ID", PROCESS_A, 3L)
            val third = sessionLog(root, "2026-09-24T12-00-00.000Z_4_$THIRD_RUN_ID", PROCESS_A, 4L)
            val heapDump = checkNotNull(first.parentFile).resolve("retained-1727172000000-LeakedActivity-1.hprof")
                .apply { writeBytes(byteArrayOf(9, 8, 7)) }

            val exported = SessionArchiveExporter.export(root, destination, snapshot = null)

            assertEquals(1, exported.size)
            assertEquals(
                setOf(
                    "$PROCESS_A/${first.name}",
                    "$PROCESS_A/${second.name}",
                    "$PROCESS_A/${third.name}",
                    "$PROCESS_A/${heapDump.name}",
                ),
                archiveEntries(File(exported.single())),
            )
            assertTrue(first.isFile)
            assertTrue(second.isFile)
            assertTrue(third.isFile)
            assertTrue(heapDump.isFile)
        } finally {
            root.deleteRecursively()
            destination.deleteRecursively()
        }
    }

    @Test
    fun compactExportGroupsPublishedAndActiveEpochsWithoutHeapDump() {
        val root = tempDir("published-epoch-root")
        val destination = tempDir("published-epoch-export")
        try {
            val historical = root.resolve("2026-09-23T10-00-00.000Z_1_$HISTORICAL_RUN_ID.jhlog.zip")
            val oldLog = "jh-session-log.2026-09-23.$HISTORICAL_RUN_ID.1.jhlog"
            val heapName = "retained-1727172000000-LeakedActivity-1.hprof"
            ZipOutputStream(historical.outputStream()).use { archive ->
                archive.putNextEntry(ZipEntry("$PROCESS_A/$oldLog"))
                archive.write(byteArrayOf(1))
                archive.closeEntry()
                archive.putNextEntry(ZipEntry("$PROCESS_A/$heapName"))
                archive.write(byteArrayOf(2))
                archive.closeEntry()
            }
            val active = sessionLog(root, "2026-09-24T10-00-00.000Z_2_$CURRENT_RUN_ID", PROCESS_A, 2L)

            val exported = SessionArchiveExporter.export(root, destination, snapshot = null, includeHeapDumps = false)

            assertEquals(1, exported.size)
            assertEquals(setOf("$PROCESS_A/$oldLog", "$PROCESS_A/${active.name}"), archiveEntries(File(exported.single())))
            assertTrue(archiveEntries(historical).contains("$PROCESS_A/$heapName"))
        } finally {
            root.deleteRecursively()
            destination.deleteRecursively()
        }
    }

    @Test
    fun duplicateArtifactNamesKeepEpochsSeparateInsteadOfFailingExport() {
        val root = tempDir("duplicate-epoch-root")
        val destination = tempDir("duplicate-epoch-export")
        try {
            val first = sessionLog(root, "2026-09-24T10-00-00.000Z_2_$CURRENT_RUN_ID", PROCESS_A, 2L)
            val second = sessionLog(root, "2026-09-24T11-00-00.000Z_3_$OTHER_RUN_ID", PROCESS_A, 3L)
            val heapName = "retained-1727172000000-LeakedActivity-1.hprof"
            checkNotNull(first.parentFile).resolve(heapName).writeBytes(byteArrayOf(1))
            checkNotNull(second.parentFile).resolve(heapName).writeBytes(byteArrayOf(2))

            val exported = SessionArchiveExporter.export(root, destination, snapshot = null)

            assertEquals(2, exported.size)
            assertEquals(setOf("$PROCESS_A/${first.name}"), archiveEntries(File(exported[0])))
            assertEquals(setOf("$PROCESS_A/${second.name}", "$PROCESS_A/$heapName"), archiveEntries(File(exported[1])))
        } finally {
            root.deleteRecursively()
            destination.deleteRecursively()
        }
    }

    @Test
    fun exportsUnarchivedSessionDirectoriesWhenCoordinatedSnapshotIsUnavailable() {
        val root = tempDir("unarchived-root")
        val destination = tempDir("unarchived-export")
        try {
            val first = sessionLog(root, "2026-09-24T10-00-00.000Z_2_$CURRENT_RUN_ID", PROCESS_A, 2L)
            val second = sessionLog(root, "2026-09-24T11-00-00.000Z_3_$OTHER_RUN_ID", PROCESS_B, 3L)

            val exported = SessionArchiveExporter.export(root, destination, snapshot = null)

            assertEquals(2, exported.size)
            assertEquals(setOf("$PROCESS_A/${first.name}"), archiveEntries(File(exported[0])))
            assertEquals(setOf("$PROCESS_B/${second.name}"), archiveEntries(File(exported[1])))
            assertTrue(first.isFile)
            assertTrue(second.isFile)
        } finally {
            root.deleteRecursively()
            destination.deleteRecursively()
        }
    }

    @Test
    fun lightweightTransportKeepsLogsWithoutCopyingLargeHeapDump() {
        val root = tempDir("light-root")
        val destination = tempDir("light-export")
        try {
            val log = sessionLog(root, "2026-09-24T10-00-00.000Z_2_$CURRENT_RUN_ID", PROCESS_A, 2L)
            val heap = checkNotNull(log.parentFile).resolve("retained-1727172000000-LeakedActivity-1.hprof")
                .apply { writeBytes(ByteArray(4096)) }

            val exported = SessionArchiveExporter.export(root, destination, snapshot = null, includeHeapDumps = false)

            assertEquals(setOf("$PROCESS_A/${log.name}"), archiveEntries(File(exported.single())))
            assertTrue(heap.isFile)
        } finally {
            root.deleteRecursively()
            destination.deleteRecursively()
        }
    }

    @Test
    fun lightweightTransportRemovesHeapDumpsFromPublishedArchives() {
        val root = tempDir("historical-light-root")
        val destination = tempDir("historical-light-export")
        try {
            val historical = root.resolve("2026-09-23T10-00-00.000Z_1_$HISTORICAL_RUN_ID.jhlog.zip")
            val logName = "jh-session-log.2026-09-23.$HISTORICAL_RUN_ID.1.jhlog"
            ZipOutputStream(historical.outputStream()).use { archive ->
                archive.putNextEntry(ZipEntry("$PROCESS_A/$logName"))
                archive.write(byteArrayOf(1, 2, 3))
                archive.closeEntry()
                archive.putNextEntry(ZipEntry("$PROCESS_A/retained-1727172000000-LeakedActivity-1.hprof"))
                archive.write(ByteArray(4096))
                archive.closeEntry()
            }

            val exported = SessionArchiveExporter.export(root, destination, snapshot = null, includeHeapDumps = false)

            assertEquals(setOf("$PROCESS_A/$logName"), archiveEntries(File(exported.single())))
            assertEquals(2, archiveEntries(historical).size)
        } finally {
            root.deleteRecursively()
            destination.deleteRecursively()
        }
    }

    @Test
    fun coordinatedSnapshotIncludesEarlierSegmentsButNotNewerActiveSegment() {
        val root = tempDir("frontier-root")
        val destination = tempDir("frontier-export")
        try {
            val session = root.resolve("2026-09-24T10-00-00.000Z_2_$CURRENT_RUN_ID")
            val process = session.resolve(PROCESS_A).apply { mkdirs() }
            val base = "jh-session-log.2026-09-24.$CURRENT_RUN_ID.2"
            val first = process.resolve("$base.jhlog").apply { writeBytes(byteArrayOf(1)) }
            val second = process.resolve("$base-1.jhlog").apply { writeBytes(byteArrayOf(2)) }
            val frontier = process.resolve("$base-2.jhlog").apply { writeBytes(byteArrayOf(3)) }
            process.resolve("$base-3.jhlog").writeBytes(byteArrayOf(4))
            val closedProcessLog = session.resolve(PROCESS_B).apply { mkdirs() }
                .resolve("$base.jhlog").apply { writeBytes(byteArrayOf(5)) }

            val exported = SessionArchiveExporter.export(
                root,
                destination,
                JankHunterLogSnapshot(42L, listOf(frontier.path), processCount = 1),
                includeHeapDumps = false,
            )

            assertEquals(
                setOf(
                    "$PROCESS_A/${first.name}",
                    "$PROCESS_A/${second.name}",
                    "$PROCESS_A/${frontier.name}",
                    "$PROCESS_B/${closedProcessLog.name}",
                ),
                archiveEntries(File(exported.single())),
            )
        } finally {
            root.deleteRecursively()
            destination.deleteRecursively()
        }
    }

    @Test
    fun exportsRetainedHistoryWhenThereIsNoActiveRuntimeSnapshot() {
        val root = tempDir("inactive-root")
        val destination = tempDir("inactive-export")
        try {
            val historical = root.resolve("2026-09-23T10-00-00.000Z_1_$HISTORICAL_RUN_ID.jhlog.zip")
            ZipOutputStream(historical.outputStream()).use { zip ->
                zip.putNextEntry(ZipEntry("$PROCESS_A/jh-session-log.2026-09-23.$HISTORICAL_RUN_ID.1.jhlog"))
                zip.write(HISTORICAL_ARCHIVE_BYTES)
                zip.closeEntry()
            }

            val exported = SessionArchiveExporter.export(root, destination, snapshot = null)

            assertEquals(listOf(destination.resolve(historical.name).canonicalPath), exported)
            assertArrayEquals(historical.readBytes(), destination.resolve(historical.name).readBytes())
        } finally {
            root.deleteRecursively()
            destination.deleteRecursively()
        }
    }

    @Test
    fun exportsOnePortableFilePerSessionWithCurrentProcessesAndHeapDumpGrouped() {
        val root = tempDir("root")
        val destination = tempDir("export")
        try {
            val historical = root.resolve("2026-09-23T10-00-00.000Z_1_$HISTORICAL_RUN_ID.jhlog.zip")
            val historicalEntry = "00112233445566778899aabbccddeeff/jh-session-log.2026-09-23.$HISTORICAL_RUN_ID.1.jhlog"
            ZipOutputStream(historical.outputStream()).use { zip ->
                zip.putNextEntry(ZipEntry(historicalEntry))
                zip.write(HISTORICAL_ARCHIVE_BYTES)
                zip.closeEntry()
            }
            val session = root.resolve("2026-09-24T10-00-00.000Z_2_$CURRENT_RUN_ID")
            val firstProcess = session.resolve(PROCESS_A).apply { mkdirs() }
            val secondProcess = session.resolve(PROCESS_B).apply { mkdirs() }
            val firstLog = firstProcess.resolve("jh-session-log.2026-09-24.$CURRENT_RUN_ID.2.jhlog")
                .apply { writeBytes(byteArrayOf(1, 2, 3)) }
            val secondLog = secondProcess.resolve("jh-session-log.2026-09-24.$CURRENT_RUN_ID.2-1.jhlog")
                .apply { writeBytes(byteArrayOf(4, 5)) }
            val heapDump = firstProcess.resolve("retained-1727172000000-LeakedActivity-1.hprof")
                .apply { writeBytes(byteArrayOf(6, 7, 8, 9)) }

            val exported = SessionArchiveExporter.export(
                root = root,
                destination = destination,
                snapshot = JankHunterLogSnapshot(42L, listOf(secondLog.path, firstLog.path), processCount = 2),
            )

            assertEquals(2, exported.size)
            val copiedHistorical = destination.resolve(historical.name)
            ZipFile(copiedHistorical).use { zip ->
                assertEquals(setOf(historicalEntry), archiveEntries(copiedHistorical))
                assertArrayEquals(HISTORICAL_ARCHIVE_BYTES, zip.getInputStream(zip.getEntry(historicalEntry)).use { it.readBytes() })
            }
            val current = destination.resolve("${session.name}.jhlog.zip")
            assertTrue(current.isFile)
            ZipFile(current).use { zip ->
                val names = zip.entries().asSequence().map { it.name }.toList()
                assertEquals(
                    listOf(
                        "$PROCESS_A/${firstLog.name}",
                        "$PROCESS_A/${heapDump.name}",
                        "$PROCESS_B/${secondLog.name}",
                    ),
                    names,
                )
                assertArrayEquals(firstLog.readBytes(), zip.getInputStream(zip.getEntry(names[0])).use { it.readBytes() })
                assertArrayEquals(heapDump.readBytes(), zip.getInputStream(zip.getEntry(names[1])).use { it.readBytes() })
                assertArrayEquals(secondLog.readBytes(), zip.getInputStream(zip.getEntry(names[2])).use { it.readBytes() })
            }
            assertTrue(firstLog.isFile)
            assertTrue(secondLog.isFile)
            assertTrue(heapDump.isFile)
            assertFalse(destination.listFiles().orEmpty().any { it.name.endsWith(".tmp") })
        } finally {
            root.deleteRecursively()
            destination.deleteRecursively()
        }
    }

    @Test
    fun rejectsSnapshotThatMixesSessionDirectories() {
        val root = tempDir("mixed-root")
        val destination = tempDir("mixed-export")
        try {
            val first = sessionLog(root, "2026-09-24T10-00-00.000Z_2_$CURRENT_RUN_ID", PROCESS_A, 2L)
            val second = sessionLog(root, "2026-09-24T11-00-00.000Z_3_$OTHER_RUN_ID", PROCESS_B, 3L)

            assertThrows(IllegalArgumentException::class.java) {
                SessionArchiveExporter.export(
                    root,
                    destination,
                    JankHunterLogSnapshot(42L, listOf(first.path, second.path), processCount = 2),
                )
            }
            assertTrue(destination.listFiles().isNullOrEmpty())
        } finally {
            root.deleteRecursively()
            destination.deleteRecursively()
        }
    }

    private fun sessionLog(root: File, sessionName: String, process: String, dailyIndex: Long): File {
        val runId = sessionName.substringAfterLast('_')
        return root.resolve(sessionName).resolve(process).apply { mkdirs() }
            .resolve("jh-session-log.2026-09-24.$runId.$dailyIndex.jhlog")
            .apply { writeBytes(byteArrayOf(1)) }
    }

    private fun tempDir(name: String): File = Files.createTempDirectory("jankhunter-$name").toFile()

    private fun archiveEntries(file: File): Set<String> = ZipFile(file).use { archive ->
        archive.entries().asSequence().map { it.name }.toSet()
    }

    private companion object {
        val HISTORICAL_ARCHIVE_BYTES = byteArrayOf(11, 12, 13)
        const val HISTORICAL_RUN_ID = "00112233445566778899aabbccddeeff"
        const val CURRENT_RUN_ID = "102132435465768798a9babcbddcedfe"
        const val OTHER_RUN_ID = "2031425364758697a8b9cadbecfd0e1f"
        const val THIRD_RUN_ID = "5061728394a5b6c7d8e9fa0b1c2d3e4f"
        const val PROCESS_A = "30415263748596a7b8c9daebfc0d1e2f"
        const val PROCESS_B = "405162738495a6b7c8d9eafb0c1d2e3f"
    }
}
