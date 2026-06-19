package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import java.nio.file.Files

class ClassGraphWriterTest {
    @Test
    fun writesJsonlClassGraphShardRecords() {
        val directory = Files.createTempDirectory("jankhunter-class-graph").toFile()

        ClassGraphWriter.write(
            directory.absolutePath,
            "com/example/FeedPresenter",
            mapOf(
                ClassGraphEdgeKey(
                    caller = "load()V",
                    calleeClass = "com.example.FeedRepository",
                    calleeMethod = "refresh",
                ) to 2,
            ),
        )

        val text = InstrumentationArtifactFiles.readJsonlLines(directory).joinToString("\n")
        assertTrue(text.contains("\"class\":\"com.example.FeedPresenter\""))
        assertTrue(text.contains("\"calleeClass\":\"com.example.FeedRepository\""))
        assertTrue(text.contains("\"calleeMethod\":\"refresh\""))
        assertTrue(text.contains("\"count\":2"))
        assertEquals(1, text.lines().filter(String::isNotBlank).size)
    }

    @Test
    fun repeatedClassShardWriteReplacesPriorClassRecord() {
        val directory = Files.createTempDirectory("jankhunter-class-graph").toFile()

        ClassGraphWriter.write(
            directory.absolutePath,
            "com/example/Presenter",
            mapOf(
                ClassGraphEdgeKey(
                    caller = "first()V",
                    calleeClass = "com.example.Repository",
                    calleeMethod = "first",
                ) to 1,
            ),
        )

        ClassGraphWriter.write(
            directory.absolutePath,
            "com/example/Presenter",
            mapOf(
                ClassGraphEdgeKey(
                    caller = "second()V",
                    calleeClass = "com.example.Repository",
                    calleeMethod = "second",
                ) to 1,
            ),
        )

        val text = InstrumentationArtifactFiles.readJsonlLines(directory).joinToString("\n")
        assertFalse(text.contains("first()V"))
        assertTrue(text.contains("second()V"))
        assertEquals(1, text.lines().filter(String::isNotBlank).size)
    }

    @Test
    fun filtersPlatformOwners() {
        assertTrue(ClassGraphWriter.isApplicationLike("com/example/Checkout"))
        assertFalse(ClassGraphWriter.isApplicationLike("android/view/View"))
        assertFalse(ClassGraphWriter.isApplicationLike("kotlinx/coroutines/BuildersKt"))
        assertFalse(ClassGraphWriter.isApplicationLike("io/jankhunter/runtime/JankHunter"))
    }
}
