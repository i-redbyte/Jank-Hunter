package io.jankhunter.runtime

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class HandlerWrapperRegistryTest {
    @Test
    fun removalUsesShardOwnedScratchInsteadOfAllocatingKeyCopies() {
        val shardType = Class.forName("io.jankhunter.runtime.HandlerWrapperRegistry\$Shard")

        assertTrue(shardType.declaredFields.any { field -> field.name == "keyScratch" })
    }

    @Test
    fun wrapperLookupReturnsFinalArrayWithoutIntermediateCollectionContract() {
        val method = HandlerWrapperRegistry::class.java.getDeclaredMethod(
            "wrappers",
            Any::class.java,
            Runnable::class.java,
            Any::class.java,
        )

        assertEquals(Array<Runnable>::class.java, method.returnType)
    }

    @Test
    fun wrappersAreResolvedByHandlerAndRunnableIdentity() {
        val dropped = mutableListOf<HandlerWrapperLoss>()
        val registry = HandlerWrapperRegistry(dropped::add)
        val handlerA = Any()
        val handlerB = Any()
        val original = Runnable {}
        val wrapperA = Runnable {}
        val wrapperB = Runnable {}
        val tokenA = Any()
        val tokenB = Any()

        assertTrue(registry.register(handlerA, original, tokenA, wrapperA, maxEntries = 10, maxWrappers = 10))
        assertTrue(registry.register(handlerB, original, tokenB, wrapperB, maxEntries = 10, maxWrappers = 10))

        assertEquals(listOf(wrapperA), registry.wrappers(handlerA, original, tokenA).toList())
        assertEquals(listOf(wrapperB), registry.wrappers(handlerB, original, tokenB).toList())
        assertTrue(registry.wrappers(handlerA, original, tokenB).isEmpty())
        assertTrue(dropped.isEmpty())
    }

    @Test
    fun unregisterByWrapperOnlyRemovesMatchingOriginalWrapper() {
        val registry = HandlerWrapperRegistry(droppedCounter = { })
        val handlerA = Any()
        val handlerB = Any()
        val original = Runnable {}
        val wrapperA = Runnable {}
        val wrapperB = Runnable {}

        assertTrue(registry.register(handlerA, original, null, wrapperA, maxEntries = 10, maxWrappers = 10))
        assertTrue(registry.register(handlerB, original, null, wrapperB, maxEntries = 10, maxWrappers = 10))

        registry.unregister(original, wrapperA)

        assertTrue(registry.wrappers(handlerA, original, null).isEmpty())
        assertEquals(listOf(wrapperB), registry.wrappers(handlerB, original, null).toList())
    }

    @Test
    fun unregisterHandlerTokenUsesHandlerScope() {
        val registry = HandlerWrapperRegistry(droppedCounter = { })
        val handlerA = Any()
        val handlerB = Any()
        val token = Any()
        val originalA = Runnable {}
        val originalB = Runnable {}
        val wrapperA = Runnable {}
        val wrapperB = Runnable {}

        assertTrue(registry.register(handlerA, originalA, token, wrapperA, maxEntries = 10, maxWrappers = 10))
        assertTrue(registry.register(handlerB, originalB, token, wrapperB, maxEntries = 10, maxWrappers = 10))

        registry.unregister(handlerA, token)

        assertTrue(registry.wrappers(handlerA, originalA, token).isEmpty())
        assertEquals(listOf(wrapperB), registry.wrappers(handlerB, originalB, token).toList())
    }

    @Test
    fun nullTokenUnregisterRemovesAllWrappersForRunnable() {
        val registry = HandlerWrapperRegistry(droppedCounter = { })
        val handler = Any()
        val original = Runnable {}
        val wrapperA = Runnable {}
        val wrapperB = Runnable {}

        assertTrue(registry.register(handler, original, Any(), wrapperA, maxEntries = 10, maxWrappers = 10))
        assertTrue(registry.register(handler, original, Any(), wrapperB, maxEntries = 10, maxWrappers = 10))

        registry.unregister(handler, original, null)

        assertTrue(registry.wrappers(handler, original, null).isEmpty())
    }

    @Test
    fun entryAndWrapperLimitsArePreserved() {
        val dropped = mutableListOf<HandlerWrapperLoss>()
        val registry = HandlerWrapperRegistry(dropped::add)
        val handler = Any()
        val original = Runnable {}
        val wrapper = Runnable {}

        assertTrue(registry.register(handler, original, null, wrapper, maxEntries = 1, maxWrappers = 1))
        assertFalse(registry.register(handler, original, null, Runnable {}, maxEntries = 1, maxWrappers = 1))
        assertFalse(registry.register(Any(), Runnable {}, null, Runnable {}, maxEntries = 1, maxWrappers = 1))

        assertEquals(
            listOf(
                HandlerWrapperLoss.WRAPPER_LIMIT,
                HandlerWrapperLoss.ENTRY_LIMIT,
            ),
            dropped,
        )
        assertSame(wrapper, registry.wrappers(handler, original, null).single())
    }

    @Test
    fun exactAdmissionStillHonorsHardMemoryLimits() {
        val dropped = mutableListOf<HandlerWrapperLoss>()
        val registry = HandlerWrapperRegistry(dropped::add) { true }
        val handler = Any()
        val original = Runnable {}
        val first = Runnable {}
        val second = Runnable {}

        assertTrue(registry.register(handler, original, null, first, maxEntries = 1, maxWrappers = 1))
        assertFalse(registry.register(handler, original, null, second, maxEntries = 1, maxWrappers = 1))
        assertFalse(registry.register(Any(), Runnable {}, null, Runnable {}, maxEntries = 1, maxWrappers = 1))
        assertEquals(listOf(first), registry.wrappers(handler, original, null).toList())
        assertEquals(listOf(HandlerWrapperLoss.WRAPPER_LIMIT, HandlerWrapperLoss.ENTRY_LIMIT), dropped)
    }
}
