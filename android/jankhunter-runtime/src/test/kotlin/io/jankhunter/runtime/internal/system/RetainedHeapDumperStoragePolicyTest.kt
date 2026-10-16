package io.jankhunter.runtime.internal.system

import io.jankhunter.runtime.JankHunterStoragePolicy
import io.jankhunter.runtime.internal.io.SessionArtifactPath
import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

class RetainedHeapDumperStoragePolicyTest {
    @get:Rule val temporary = TemporaryFolder()
    private var writes = 0

    @Test fun disabledExtensionDoesNotInvokeAndroidHeapWriter() {
        val result = dumper(policy(extensions = setOf("jhlog"))).maybeDump("Owner", null, 1L, 1L)
        assertTrue(result is RetainedHeapDumper.Result.Skipped)
        assertEquals(0, writes)
        assertFalse(hasHeap())
    }

    @Test fun unexemptHeapOverFileLimitIsRejectedWithoutPublishedPartial() {
        val result = dumper(policy(fileLimit = 8L, exemptions = emptySet())).maybeDump("Owner", null, 1L, 1L)
        assertTrue(result is RetainedHeapDumper.Result.Skipped)
        assertFalse(hasHeap())
        assertFalse(hasPending())
    }

    @Test fun fileExemptionDoesNotBypassTotalStorageLimit() {
        val result = dumper(policy(fileLimit = 8L, totalLimit = 16L)).maybeDump("Owner", null, 1L, 1L)
        assertTrue(result is RetainedHeapDumper.Result.Skipped)
        assertFalse(hasHeap())
        assertFalse(hasPending())
    }

    @Test fun eligibleHeapIsPublishedInOwningSessionWithoutTruncation() {
        val result = dumper(policy(fileLimit = 8L)).maybeDump("Owner", null, 1L, 1L)
        assertTrue(result is RetainedHeapDumper.Result.Dumped)
        val file = (result as RetainedHeapDumper.Result.Dumped).file
        assertEquals(32L, file.length())
        assertTrue(file.readBytes().all { it == 7.toByte() })
        assertFalse(hasPending())
    }

    private fun policy(
        fileLimit: Long = 1_024L,
        totalLimit: Long = 4_096L,
        extensions: Set<String> = setOf("jhlog", "hprof"),
        exemptions: Set<String> = setOf("hprof"),
    ) = JankHunterStoragePolicy(temporary.root, fileLimit, totalLimit, extensions, exemptions, 32_768, true)

    private fun dumper(policy: JankHunterStoragePolicy): RetainedHeapDumper {
        val scope = SessionArtifactPath.scope(temporary.root, 1_000L, 0L, ByteArray(16) { 1 }, ByteArray(16) { 2 })
        return RetainedHeapDumper(
            scope.processDirectory,
            minIntervalMs = 0L,
            maxDumpCount = 1,
            dumpHprof = { path -> writes++; File(path).writeBytes(ByteArray(32) { 7 }) },
            storagePolicy = policy,
            storageRoot = temporary.root,
        )
    }

    private fun hasHeap() = temporary.root.walkTopDown().any { it.extension == "hprof" }
    private fun hasPending() = temporary.root.walkTopDown().any { it.extension == "pending" }
}
