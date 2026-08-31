package io.jankhunter.runtime

import java.io.FileNotFoundException
import java.io.FileOutputStream
import java.nio.file.Files
import org.junit.After
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertThrows
import org.junit.Before
import org.junit.Test

class JankHunterIOHooksTest {
    private lateinit var directory: java.io.File

    @Before
    fun setUp() {
        JankHunter.shutdown()
        directory = Files.createTempDirectory("jankhunter-io-hooks").toFile()
    }

    @After
    fun tearDown() {
        JankHunter.shutdown()
        directory.deleteRecursively()
    }

    @Test
    fun inactiveRuntimePreservesReviewedFileOperations() {
        val file = directory.resolve("payload.bin")

        JankHunterIOHooks.writeFileBytes(file, byteArrayOf(1, 2), SOURCE_ID, SOURCE_NAME)
        JankHunterIOHooks.appendFileBytes(file, byteArrayOf(3), SOURCE_ID, SOURCE_NAME)

        assertArrayEquals(
            byteArrayOf(1, 2, 3),
            JankHunterIOHooks.readFileBytes(file, SOURCE_ID, SOURCE_NAME),
        )
        FileOutputStream(file, true).use { output ->
            JankHunterIOHooks.syncFileDescriptor(output.fd, SOURCE_ID, SOURCE_NAME)
            JankHunterIOHooks.forceFileChannel(output.channel, true, SOURCE_ID, SOURCE_NAME)
        }
    }

    @Test
    fun inactiveRuntimePreservesOriginalFailure() {
        val missing = directory.resolve("missing.bin")

        assertThrows(FileNotFoundException::class.java) {
            JankHunterIOHooks.readFileBytes(missing, SOURCE_ID, SOURCE_NAME)
        }
    }

    private companion object {
        private const val SOURCE_ID = 42L
        private const val SOURCE_NAME = "example.Owner#method()V"
    }
}
