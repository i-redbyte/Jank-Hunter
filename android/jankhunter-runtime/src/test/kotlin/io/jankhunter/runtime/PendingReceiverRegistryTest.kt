package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.Jhlog
import java.lang.ref.WeakReference
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PendingReceiverRegistryTest {
    @Test
    fun registryUsesPendingResultIdentityAndConsumesSnapshotExactlyOnce() {
        val registry = PendingReceiverRegistry(capacity = 4)
        val pendingResult = Any()
        val snapshot = PendingReceiverSnapshot(
            token = 7L,
            componentId = 11L,
            componentName = "example.BootReceiver",
            action = "example.SYNC",
            instanceId = 13L,
            flags = Jhlog.COMPONENT_FLAG_ORDERED,
        )

        assertTrue(registry.register(pendingResult, snapshot))
        assertEquals(snapshot, registry.complete(pendingResult))
        assertNull(registry.complete(pendingResult))
        assertEquals(0, registry.retainedEntryCount())
    }

    @Test
    fun receiverFlagsKeepOrderedStickyAndAsyncDimensionsIndependent() {
        assertEquals(
            Jhlog.COMPONENT_FLAG_ORDERED or Jhlog.COMPONENT_FLAG_STICKY or Jhlog.COMPONENT_FLAG_ASYNC,
            receiverComponentFlags(ordered = true, sticky = true, async = true),
        )
        assertEquals(0L, receiverComponentFlags(ordered = false, sticky = false, async = false))
    }

    @Test
    fun registryBoundsMemoryAndReportsEvictionAffectedMisses() {
        val evictions = AtomicInteger()
        val misses = AtomicInteger()
        val registry = PendingReceiverRegistry(
            capacity = 2,
            onEviction = evictions::incrementAndGet,
            onResolutionMissAfterEviction = misses::incrementAndGet,
        )
        val pendingResults = arrayOf(Any(), Any(), Any())
        pendingResults.forEachIndexed { index, pendingResult ->
            registry.register(
                pendingResult,
                PendingReceiverSnapshot(index + 1L, 1L, "receiver", null, 1L, 0L),
            )
        }

        val evicted = pendingResults.first { registry.complete(it) == null }
        registry.complete(evicted)

        assertEquals(1, evictions.get())
        assertTrue(misses.get() >= 1)
        assertTrue(registry.retainedEntryCount() <= 2)
        val references = PendingReceiverRegistry::class.java.getDeclaredField("references").apply {
            isAccessible = true
        }.get(registry) as Array<*>
        assertTrue(references.filterNotNull().all { it is WeakReference<*> })
    }
}
