package io.jankhunter.runtime.internal.io

import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

class SessionStorageDirectoryTest {
    @get:Rule val temporary = TemporaryFolder()

    @Test fun policyDirectoryAppliesBeforeFirstRecording() {
        val state = temporary.newFolder("state")
        val fallback = temporary.newFolder("fallback")
        val requested = File(temporary.root, "configured")
        SessionStorageDirectory(state).use { assertEquals(requested.canonicalFile, it.resolve(fallback, requested)) }
    }

    @Test fun latePolicyKeepsCurrentProcessRootAndAppliesOnNextLaunch() {
        val state = temporary.newFolder("state")
        val fallback = temporary.newFolder("fallback")
        val requested = File(temporary.root, "configured")
        val first = SessionStorageDirectory(state)
        assertEquals(fallback.canonicalFile, first.resolve(fallback, null))
        assertEquals(fallback.canonicalFile, first.resolve(fallback, requested))
        first.close()
        SessionStorageDirectory(state).use { assertEquals(requested.canonicalFile, it.resolve(fallback, null)) }
    }

    @Test fun secondaryProcessJoinsExistingRootAfterLatePolicyChange() {
        val state = temporary.newFolder("state")
        val fallback = temporary.newFolder("fallback")
        val requested = File(temporary.root, "configured")
        val first = SessionStorageDirectory(state)
        first.resolve(fallback, null)
        ProcessRunCohort.join(fallback, "2026-09-25").use {
            first.resolve(fallback, requested)
            SessionStorageDirectory(state).use { assertEquals(fallback.canonicalFile, it.resolve(fallback, requested)) }
        }
        first.close()
        SessionStorageDirectory(state).use { assertEquals(requested.canonicalFile, it.resolve(fallback, null)) }
    }

    @Test fun selectionLeasePreventsMigrationBeforeWriterCohortJoins() {
        val state = temporary.newFolder("state")
        val fallback = temporary.newFolder("fallback")
        val requested = temporary.newFolder("configured")
        val first = SessionStorageDirectory(state)
        first.resolve(fallback, null)
        val secondary = SessionStorageDirectory(state)
        assertEquals(fallback.canonicalFile, secondary.resolve(fallback, requested))
        first.close()
        secondary.close()
        SessionStorageDirectory(state).use { assertEquals(requested.canonicalFile, it.resolve(fallback, null)) }
    }

    @Test fun changedStandaloneFallbackIsNotShadowedByPersistentState() {
        val state = temporary.newFolder("state")
        val fallback = temporary.newFolder("fallback")
        SessionStorageDirectory(state).use { it.resolve(fallback, null) }
        val changed = temporary.newFolder("changed")
        SessionStorageDirectory(state).use { assertEquals(changed.canonicalFile, it.resolve(changed, null)) }
    }

    @Test fun coldMigrationPreservesHistoryAndForeignFilesAndRemovesOldManagedCopy() {
        val state = temporary.newFolder("state")
        val fallback = temporary.newFolder("fallback")
        val requested = temporary.newFolder("configured")
        val archive = File(fallback, SESSION + ".jhlog.zip").apply { writeBytes(byteArrayOf(1, 2, 3)) }
        val foreign = File(fallback, "user.txt").apply { writeText("keep") }
        SessionStorageDirectory(state).use { it.resolve(fallback, null); it.resolve(fallback, requested) }
        SessionStorageDirectory(state).use { assertEquals(requested.canonicalFile, it.resolve(fallback, null)) }
        org.junit.Assert.assertArrayEquals(byteArrayOf(1, 2, 3), File(requested, archive.name).readBytes())
        assertFalse(archive.exists())
        org.junit.Assert.assertTrue(foreign.exists())
    }

    @Test fun collisionUnknownSessionArtifactAndActiveExportDeferMigration() {
        for (reason in listOf("collision", "unknown", "export")) {
            val state = temporary.newFolder("state-$reason")
            val fallback = temporary.newFolder("fallback-$reason")
            val requested = temporary.newFolder("configured-$reason")
            val source = File(fallback, SESSION + ".jhlog.zip").apply { writeText("original") }
            if (reason == "collision") File(requested, source.name).writeText("different")
            if (reason == "unknown") File(fallback, "$SESSION/foreign.txt").apply { checkNotNull(parentFile).mkdirs(); writeText("keep") }
            val lease = if (reason == "export") SessionArtifactReadLeases.acquire(fallback) else null
            try {
                SessionStorageDirectory(state).use { assertEquals(fallback.canonicalFile, it.resolve(fallback, requested)) }
                assertEquals("original", source.readText())
            } finally { lease?.close() }
        }
    }

    @Test fun failedStatePublicationLeavesAllSourceFilesUntouched() {
        val source = temporary.newFolder("source")
        val destination = temporary.newFolder("destination")
        val original = File(source, SESSION + ".jhlog.zip").apply { writeText("keep") }
        org.junit.Assert.assertThrows(java.io.IOException::class.java) {
            SessionStorageMigration.migrate(source.canonicalFile, destination.canonicalFile) {
                throw java.io.IOException("state publish failed")
            }
        }
        assertEquals("keep", original.readText())
        assertFalse(File(destination, original.name).exists())
    }

    @Test fun migrationPreservesDailySequenceAfterArchivedLogsLeaveTheSourceTree() {
        val state = temporary.newFolder("state")
        val fallback = temporary.newFolder("fallback")
        val requested = temporary.newFolder("configured")
        val firstIndex = ProcessRunCohort.join(fallback, "2026-09-25").use { it.identity().dailySessionIndex }
        SessionStorageDirectory(state).use { it.resolve(fallback, requested) }
        val nextIndex = ProcessRunCohort.join(requested, "2026-09-25").use { it.identity().dailySessionIndex }
        assertEquals(firstIndex + 1, nextIndex)
    }

    @Test fun committedMigrationResumesCleanupAfterProcessDeathAndClearsItsMarker() {
        val state = temporary.newFolder("state").canonicalFile
        val source = temporary.newFolder("source").canonicalFile
        val destination = temporary.newFolder("destination").canonicalFile
        val old = File(source, SESSION + ".jhlog.zip").apply { writeText("history") }
        old.copyTo(File(destination, old.name))
        java.io.DataOutputStream(File(state, "storage-directory.bin").outputStream()).use { output ->
            output.writeInt(0x4a485345)
            output.writeUTF(destination.path)
            output.writeUTF(destination.path)
            output.writeUTF(source.path)
            output.writeUTF(source.path)
        }
        SessionStorageDirectory(state).use { assertEquals(destination, it.resolve(source, null)) }
        assertFalse(old.exists())
        assertEquals("history", File(destination, old.name).readText())
        old.writeText("new unrelated source content")
        SessionStorageDirectory(state).use { assertEquals(destination, it.resolve(source, null)) }
        assertEquals("new unrelated source content", old.readText())
    }

    @Test fun interruptedPrecommitCopyResumesFromPersistedDestinationIntent() {
        val state = temporary.newFolder("state").canonicalFile
        val source = temporary.newFolder("source").canonicalFile
        val destination = temporary.newFolder("destination").canonicalFile
        val original = File(source, SESSION + ".jhlog.zip").apply { writeText("history") }
        original.copyTo(File(destination, original.name))
        java.io.DataOutputStream(File(state, "storage-directory.bin").outputStream()).use { output ->
            output.writeInt(0x4a485345)
            output.writeUTF(source.path)
            output.writeUTF(destination.path)
            output.writeUTF(source.path)
            output.writeUTF("")
        }
        SessionStorageDirectory(state).use { assertEquals(destination, it.resolve(source, null)) }
        assertFalse(original.exists())
        assertEquals("history", File(destination, original.name).readText())
    }

    @Test fun committedMigrationFinishesTransfersMissingFromDestinationAfterProcessDeath() {
        val state = temporary.newFolder("state").canonicalFile
        val source = temporary.newFolder("source").canonicalFile
        val destination = temporary.newFolder("destination").canonicalFile
        val original = File(source, SESSION + ".jhlog.zip").apply { writeText("not moved yet") }
        val alreadyMoved = File(destination, "retained-1727172000000-Test-1.hprof").apply { writeText("already moved") }
        java.io.DataOutputStream(File(state, "storage-directory.bin").outputStream()).use { output ->
            output.writeInt(0x4a485345)
            output.writeUTF(destination.path)
            output.writeUTF(destination.path)
            output.writeUTF(source.path)
            output.writeUTF(source.path)
        }
        SessionStorageDirectory(state).use { assertEquals(destination, it.resolve(source, null)) }
        assertFalse(original.exists())
        assertEquals("not moved yet", File(destination, original.name).readText())
        assertEquals("already moved", alreadyMoved.readText())
    }

    @Test fun nestedDestinationDefersMigrationWithoutMovingFiles() {
        val state = temporary.newFolder("state")
        val source = temporary.newFolder("source")
        val original = File(source, SESSION + ".jhlog.zip").apply { writeText("history") }
        SessionStorageDirectory(state).use { selector ->
            assertEquals(source.canonicalFile, selector.resolve(source, File(source, "nested")))
        }
        assertEquals("history", original.readText())
    }

    private companion object {
        const val SESSION = "2026-09-24T10-00-00.000Z_2_102132435465768798a9babcbddcedfe"
    }

    @Test fun unchangedRootDoesNotRewritePersistentState() {
        val state = temporary.newFolder("state")
        val fallback = temporary.newFolder("fallback")
        val first = SessionStorageDirectory(state)
        first.resolve(fallback, null)
        val persisted = File(state, "storage-directory.bin")
        persisted.setLastModified(1_000L)
        first.resolve(fallback, null)
        assertEquals(1_000L, persisted.lastModified())
    }

    @Test fun partialStateDoesNotBecomeArbitraryDirectory() {
        val state = temporary.newFolder("state")
        val fallback = temporary.newFolder("fallback")
        File(state, "storage-directory.bin").writeBytes(byteArrayOf(1, 2, 3))
        SessionStorageDirectory(state).use { assertEquals(fallback.canonicalFile, it.resolve(fallback, null)) }
        assertFalse(File(state, "storage-directory.pending").exists())
    }
}
