package io.jankhunter.runtime.internal.system

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class MainThreadStallEvidenceTest {
    @Test
    fun repeatedApplicationFrameWinsOverFirstTransientSample() {
        val evidence = MainThreadStallEvidence(maxSamples = 4)

        evidence.addSample(stack("com.example.Initial", "prepare", 10))
        evidence.addSample(stack("com.example.BlockingStore", "read", 20))
        evidence.addSample(stack("com.example.BlockingStore", "read", 21))

        assertTrue(evidence.stackHint.contains("com.example.BlockingStore.read"))
        assertEquals(3, evidence.sampleCount)
    }

    @Test
    fun callerChainIsPreservedAndSamplingIsBounded() {
        val evidence = MainThreadStallEvidence(maxSamples = 2)
        val infrastructure = StackTraceElement("android.os.MessageQueue", "nativePollOnce", "MessageQueue.java", 1)

        assertTrue(evidence.addSample(arrayOf(infrastructure, frame("com.example.Feed", "render", 42))))
        assertTrue(evidence.addSample(stack("com.example.Feed", "render", 43)))
        assertFalse(evidence.addSample(stack("com.example.Unbounded", "work", 99)))

        assertEquals(2, evidence.sampleCount)
        assertTrue(evidence.stackHint.contains("com.example.Feed.render"))
    }

    @Test
    fun obfuscatedLibraryFrameDoesNotBecomeOwnerAndCallerIsRetained() {
        val evidence = MainThreadStallEvidence(maxSamples = 2)
        evidence.addSample(arrayOf(frame("o0.f", "getValue", 10), frame("a.b", "render", 22)))
        assertTrue(evidence.stackHint.contains("o0.f.getValue"))
        assertTrue(evidence.stackHint.contains("a.b.render"))
    }

    @Test
    fun oversizedStackHasAnExplicitBoundedTruncationMarker() {
        val evidence = MainThreadStallEvidence(maxSamples = 2)
        evidence.addSample(Array(1000) { frame("a.b", "call$it", it) })
        assertTrue(evidence.stackHint.length <= 4096)
        assertTrue(evidence.stackHint.contains("stack truncated"))
        assertTrue(evidence.stackHint.lines().count { it.startsWith("\tat ") } <= 32)
    }

    @Test
    fun stackHonorsUtf8DictionaryBudgetWithoutLosingTruncationStatus() {
        for (budget in listOf(1, 32, 64, 1024, 4096)) {
            val evidence = MainThreadStallEvidence(maxSamples = 2, maxStackBytes = budget)
            evidence.addSample(Array(100) { frame("пример.Экран", "вызов$it", it) })
            assertTrue(evidence.stackHint.toByteArray(Charsets.UTF_8).size <= budget)
            assertTrue(evidence.stackHint.contains("stack truncated") || evidence.stackHint == "!")
        }
    }

    private fun stack(className: String, methodName: String, line: Int): Array<StackTraceElement> {
        return arrayOf(frame(className, methodName, line))
    }

    private fun frame(className: String, methodName: String, line: Int): StackTraceElement {
        return StackTraceElement(className, methodName, "Source.kt", line)
    }
}
