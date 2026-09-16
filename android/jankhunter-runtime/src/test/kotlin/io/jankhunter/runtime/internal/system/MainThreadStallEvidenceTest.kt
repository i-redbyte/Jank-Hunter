package io.jankhunter.runtime.internal.system

import io.jankhunter.runtime.JankHunterContextSnapshot
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class MainThreadStallEvidenceTest {
    @Test
    fun repeatedApplicationFrameWinsOverFirstTransientSample() {
        val evidence = MainThreadStallEvidence(maxSamples = 4)

        evidence.addSample(stack("com.example.Initial", "prepare", 10))
        evidence.addSample(stack("com.example.BlockingStore", "read", 20))
        evidence.addSample(stack("com.example.BlockingStore", "read", 21))

        assertEquals("com.example.BlockingStore", evidence.owner)
        assertTrue(evidence.stackHint.contains("com.example.BlockingStore.read"))
        assertEquals(3, evidence.sampleCount)
    }

    @Test
    fun applicationFrameIsPreferredAndSamplingIsBounded() {
        val evidence = MainThreadStallEvidence(maxSamples = 2)
        val infrastructure = StackTraceElement("android.os.MessageQueue", "nativePollOnce", "MessageQueue.java", 1)

        assertTrue(evidence.addSample(arrayOf(infrastructure, frame("com.example.Feed", "render", 42))))
        assertTrue(evidence.addSample(stack("com.example.Feed", "render", 43)))
        assertFalse(evidence.addSample(stack("com.example.Unbounded", "work", 99)))

        assertEquals(2, evidence.sampleCount)
        assertEquals("com.example.Feed", evidence.owner)
        assertTrue(evidence.stackHint.contains("com.example.Feed.render"))
    }

    @Test
    fun finalEvidenceOwnerFillsOnlyMissingContextOwner() {
        val contextWithoutOwner = JankHunterContextSnapshot(
            screen = "Feed",
            owner = null,
            initiatorPresent = true,
            initiatorId = 7L,
            initiatorName = "tap",
            operationId = 11L,
        )

        val resolved = contextWithoutOwner.withStallOwnerFallback("com.example.BlockingStore")

        assertEquals("Feed", resolved.screen)
        assertEquals("com.example.BlockingStore", resolved.owner)
        assertTrue(resolved.initiatorPresent)
        assertEquals(7L, resolved.initiatorId)
        assertEquals("tap", resolved.initiatorName)
        assertEquals(11L, resolved.operationId)

        val attributed = JankHunterContextSnapshot(screen = "Feed", owner = "FeedOperation")
        assertSame(attributed, attributed.withStallOwnerFallback("com.example.BlockingStore"))
    }

    private fun stack(className: String, methodName: String, line: Int): Array<StackTraceElement> {
        return arrayOf(frame(className, methodName, line))
    }

    private fun frame(className: String, methodName: String, line: Int): StackTraceElement {
        return StackTraceElement(className, methodName, "Source.kt", line)
    }
}
