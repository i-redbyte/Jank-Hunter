package io.jankhunter.runtime

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeWorkChainTrackerTest {
    @Test
    fun opaqueParentsAreSkippedWithoutAccessingApplicationCode() {
        val parents = object : AbstractList<Any>() {
            override val size: Int get() = error("opaque size")
            override fun get(index: Int): Any = error("opaque element")
        }
        assertFalse(RuntimeWorkChainTracker().combine(parents, Any()))
    }

    @Test
    fun declaredEdgesFollowOnlyObservedContinuationIdentity() {
        val tracker = RuntimeWorkChainTracker(capacity = 4, maxTailIds = 2)
        val root = Any()
        val next = Any()
        val edges = mutableListOf<Pair<Long, Long>>()
        assertTrue(tracker.root(root, 1, 1))
        assertTrue(tracker.root(root, 1, 2))
        assertTrue(tracker.then(root, next, 2, 1) { parent, child -> edges += parent.least to child.least })
        assertTrue(tracker.then(root, next, 2, 2) { parent, child -> edges += parent.least to child.least })
        assertEquals(listOf(1L to 1L, 2L to 1L, 1L to 2L, 2L to 2L), edges)
        assertTrue(tracker.then(next, Any(), 3, 1) { parent, child -> edges += parent.least to child.least })
        assertEquals(2L to 1L, edges.last())
    }

    @Test
    fun missingParentAndCapacityNeverInventDependencies() {
        val tracker = RuntimeWorkChainTracker(capacity = 1, maxTailIds = 1)
        val unknown = Any()
        val child = Any()
        var emitted = 0
        assertTrue(tracker.then(unknown, child, 2, 1) { _, _ -> emitted++ })
        assertEquals(0, emitted)
        assertFalse(tracker.root(Any(), 1, 1))
        assertFalse(tracker.then(unknown, child, 2, 2) { _, _ -> emitted++ })
        assertEquals(0, emitted)
        assertEquals(2L, tracker.omittedIds)
    }

    @Test
    fun separateTrackersCannotJoinTheSameObjectAcrossEpochs() {
        val root = Any()
        val next = Any()
        val oldEpoch = RuntimeWorkChainTracker()
        assertTrue(oldEpoch.root(root, 1, 1))
        val newEpoch = RuntimeWorkChainTracker()
        var emitted = false
        assertTrue(newEpoch.then(root, next, 2, 1) { _, _ -> emitted = true })
        assertFalse(emitted)
    }

    @Test
    fun combinedContinuationsRetainObservedTailsWithoutInventingMissingParents() {
        val tracker = RuntimeWorkChainTracker(capacity = 4, maxTailIds = 2)
        val left = Any()
        val right = Any()
        val combined = Any()
        assertTrue(tracker.root(left, 0, 1))
        assertTrue(tracker.root(right, 0, 2))
        assertTrue(tracker.combine(listOf(left, Any(), right), combined))
        val edges = mutableListOf<Pair<Long, Long>>()
        assertTrue(tracker.then(combined, Any(), 0, 3) { parent, child -> edges += parent.least to child.least })
        assertEquals(listOf(1L to 3L, 2L to 3L), edges)
    }

    @Test
    fun combineCapacityIsBoundedAndDoesNotEmitPartialGraph() {
        val tracker = RuntimeWorkChainTracker(capacity = 4, maxTailIds = 1)
        val left = Any()
        val right = Any()
        val combined = Any()
        assertTrue(tracker.root(left, 0, 1))
        assertTrue(tracker.root(right, 0, 2))
        assertFalse(tracker.combine(listOf(left, right), combined))
        var edges = 0
        assertTrue(tracker.then(combined, Any(), 0, 3) { _, _ -> edges++ })
        assertEquals(0, edges)
        assertEquals(1L, tracker.omittedIds)
    }
}
