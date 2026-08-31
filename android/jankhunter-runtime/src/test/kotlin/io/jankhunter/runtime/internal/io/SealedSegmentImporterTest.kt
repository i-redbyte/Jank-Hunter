package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterBinaryArtifact
import io.jankhunter.runtime.JankHunterBinaryStorage
import io.jankhunter.runtime.JankHunterBinaryWriter
import java.io.File
import java.io.IOException
import java.nio.file.Files
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class SealedSegmentImporterTest {
    @Test
    fun importPublishesVerifiedCopyAndKeepsSourceUntilCallerCommitsHandoff() {
        val root = Files.createTempDirectory("jankhunter-segment-import").toFile()
        try {
            val source = File(root, "source.jhlog").apply { writeBytes(ByteArray(4_096) { it.toByte() }) }
            val storage = TestStorage(File(root, "target"))

            val result = SealedSegmentImporter.import(source.absolutePath, storage)

            assertTrue(result.created)
            assertTrue(source.isFile)
            assertArrayEquals(source.readBytes(), File(result.path).readBytes())
            assertEquals(1, storage.artifactCommits)
            assertEquals(1, storage.protections)
            result.protection?.commit()
            assertEquals(1, storage.protectionCommits)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun importReusesAnIdenticalPublishedSegmentWithoutCreatingAnotherArtifact() {
        val root = Files.createTempDirectory("jankhunter-segment-import-idempotent").toFile()
        try {
            val bytes = byteArrayOf(1, 2, 3, 4)
            val source = File(root, "segment.jhlog").apply { writeBytes(bytes) }
            val storage = TestStorage(File(root, "target"))
            File(storage.directory, source.name).apply {
                parentFile?.mkdirs()
                writeBytes(bytes)
            }

            val result = SealedSegmentImporter.import(source.absolutePath, storage)

            assertFalse(result.created)
            assertArrayEquals(bytes, File(result.path).readBytes())
            assertEquals(0, storage.artifactCommits)
            assertEquals(1, storage.protections)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun importRejectsSameNameWithDifferentContentWithoutChangingEitherFile() {
        val root = Files.createTempDirectory("jankhunter-segment-import-collision").toFile()
        try {
            val sourceBytes = byteArrayOf(1, 2, 3)
            val targetBytes = byteArrayOf(4, 5, 6)
            val source = File(root, "segment.jhlog").apply { writeBytes(sourceBytes) }
            val storage = TestStorage(File(root, "target"))
            val target = File(storage.directory, source.name).apply {
                parentFile?.mkdirs()
                writeBytes(targetBytes)
            }

            assertThrows(IOException::class.java) {
                SealedSegmentImporter.import(source.absolutePath, storage)
            }

            assertArrayEquals(sourceBytes, source.readBytes())
            assertArrayEquals(targetBytes, target.readBytes())
            assertEquals(0, storage.protections)
            assertEquals(0, storage.artifactCommits)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun importRejectsSegmentLargerThanTargetPhysicalLimitBeforeCreatingArtifact() {
        val root = Files.createTempDirectory("jankhunter-segment-import-limit").toFile()
        try {
            val source = File(root, "segment.jhlog").apply { writeBytes(ByteArray(5)) }
            val storage = TestStorage(File(root, "target"), fileSizeLimitBytes = 4L)

            assertThrows(IOException::class.java) {
                SealedSegmentImporter.import(source.absolutePath, storage)
            }

            assertTrue(source.isFile)
            assertTrue(storage.listFiles().isEmpty())
            assertEquals(0, storage.artifactCreations)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun failedArtifactCommitRemovesUncommittedCopyAndKeepsSource() {
        val root = Files.createTempDirectory("jankhunter-segment-import-commit-failure").toFile()
        try {
            val bytes = byteArrayOf(7, 8, 9)
            val source = File(root, "segment.jhlog").apply { writeBytes(bytes) }
            val storage = TestStorage(File(root, "target"), failArtifactCommit = true)

            assertThrows(IOException::class.java) {
                SealedSegmentImporter.import(source.absolutePath, storage)
            }

            assertArrayEquals(bytes, source.readBytes())
            assertTrue(storage.listFiles().isEmpty())
            assertEquals(1, storage.protectionAborts)
            assertEquals(1, storage.artifactAborts)
        } finally {
            root.deleteRecursively()
        }
    }

    private class TestStorage(
        val directory: File,
        override val fileSizeLimitBytes: Long = Long.MAX_VALUE,
        private val failArtifactCommit: Boolean = false,
    ) : JankHunterBinaryStorage {
        override val archivesSizeLimitBytes: Long = Long.MAX_VALUE
        var artifactCreations = 0
        var artifactCommits = 0
        var artifactAborts = 0
        var protections = 0
        var protectionCommits = 0
        var protectionAborts = 0

        override fun openWriter(fileName: String): JankHunterBinaryWriter {
            throw UnsupportedOperationException("Not required by importer tests")
        }

        override fun createArtifact(fileName: String): JankHunterBinaryArtifact {
            artifactCreations++
            val file = File(directory, fileName)
            return object : JankHunterBinaryArtifact {
                override val path: String = file.absolutePath

                override fun commit() {
                    if (failArtifactCommit) throw IOException("injected artifact commit failure")
                    artifactCommits++
                }

                override fun abort() {
                    artifactAborts++
                    file.delete()
                }
            }
        }

        override fun protect(fileName: String): JankHunterBinaryArtifact {
            protections++
            val file = File(directory, fileName)
            return object : JankHunterBinaryArtifact {
                override val path: String = file.absolutePath

                override fun commit() {
                    protectionCommits++
                }

                override fun abort() {
                    protectionAborts++
                    file.delete()
                }
            }
        }

        override fun cleanup(protectedPaths: Set<String>) = Unit

        override fun listFiles(): List<String> =
            directory.listFiles().orEmpty().filter(File::isFile).map(File::getAbsolutePath)
    }
}
