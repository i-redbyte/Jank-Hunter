package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

class ClassGraphWriterTest {
    @Test
    fun writesJsonlClassGraphRecords() {
        val file = File.createTempFile("jankhunter-class-graph", ".jsonl")
        file.delete()

        ClassGraphWriter.append(
            file.absolutePath,
            "com/example/FeedPresenter",
            mapOf(
                ClassGraphEdgeKey(
                    caller = "load()V",
                    calleeClass = "com.example.FeedRepository",
                    calleeMethod = "refresh",
                ) to 2,
            ),
        )

        val text = file.readText()
        assertTrue(text.contains("\"class\":\"com.example.FeedPresenter\""))
        assertTrue(text.contains("\"calleeClass\":\"com.example.FeedRepository\""))
        assertTrue(text.contains("\"calleeMethod\":\"refresh\""))
        assertTrue(text.contains("\"count\":2"))
        assertEquals(1, text.lines().filter(String::isNotBlank).size)
    }

    @Test
    fun prepareDeletesStaleRowsForRepeatedBuildPath() {
        val file = File.createTempFile("jankhunter-class-graph", ".jsonl")
        file.delete()

        ClassGraphWriter.append(
            file.absolutePath,
            "com/example/First",
            mapOf(
                ClassGraphEdgeKey(
                    caller = "first()V",
                    calleeClass = "com.example.Repository",
                    calleeMethod = "first",
                ) to 1,
            ),
        )
        file.appendText("""{"class":"stale"}""" + "\n")

        ClassGraphWriter.prepare(file.absolutePath)
        ClassGraphWriter.append(
            file.absolutePath,
            "com/example/Second",
            mapOf(
                ClassGraphEdgeKey(
                    caller = "second()V",
                    calleeClass = "com.example.Repository",
                    calleeMethod = "second",
                ) to 1,
            ),
        )

        val text = file.readText()
        assertFalse(text.contains("stale"))
        assertFalse(text.contains("com.example.First"))
        assertTrue(text.contains("\"class\":\"com.example.Second\""))
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
