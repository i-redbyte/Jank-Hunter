package io.jankhunter.runtime.internal.io

import java.io.File
import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Test

class ExternalSegmentRegistryTest {
    @Test
    fun updatesSurviveRegistryRecreationAndRemainProcessScoped() {
        val directory = Files.createTempDirectory("jankhunter-external-registry").toFile()
        try {
            val main = ExternalSegmentRegistry(directory, "main")
            val remote = ExternalSegmentRegistry(directory, "remote")
            val first = File(directory, "external/first.jhlog").absolutePath
            val second = File(directory, "external/second.jhlog").absolutePath

            main.add(first)
            main.add(second)
            main.remove(first)
            remote.add(first)

            assertEquals(listOf(second), ExternalSegmentRegistry(directory, "main").paths())
            assertEquals(listOf(first), ExternalSegmentRegistry(directory, "remote").paths())
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun corruptedNewestSlotFallsBackToLastCompleteSnapshot() {
        val directory = Files.createTempDirectory("jankhunter-external-registry-torn-write").toFile()
        try {
            val registry = ExternalSegmentRegistry(directory, "main")
            val first = File(directory, "external/first.jhlog").absolutePath
            val second = File(directory, "external/second.jhlog").absolutePath
            registry.add(first)
            registry.add(second)
            val newestSlot = directory.listFiles { file ->
                file.name.startsWith(".jh-external-segments-") && file.name.endsWith("-1.bin")
            }.orEmpty().single()

            newestSlot.writeBytes(byteArrayOf(1, 2, 3))

            assertEquals(listOf(first), ExternalSegmentRegistry(directory, "main").paths())
        } finally {
            directory.deleteRecursively()
        }
    }
}
