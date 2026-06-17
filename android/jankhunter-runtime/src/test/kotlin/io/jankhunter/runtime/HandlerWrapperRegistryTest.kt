package io.jankhunter.runtime

import android.os.Handler
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

@Suppress("DEPRECATION")
class HandlerWrapperRegistryTest {
    @Test
    fun wrappersAreResolvedByHandlerAndRunnableIdentity() {
        val dropped = mutableListOf<String>()
        val registry = HandlerWrapperRegistry(dropped::add)
        val handlerA = Handler()
        val handlerB = Handler()
        val original = Runnable {}
        val wrapperA = Runnable {}
        val wrapperB = Runnable {}
        val tokenA = Any()
        val tokenB = Any()

        assertTrue(registry.register(handlerA, original, tokenA, wrapperA, maxEntries = 10, maxWrappers = 10))
        assertTrue(registry.register(handlerB, original, tokenB, wrapperB, maxEntries = 10, maxWrappers = 10))

        assertEquals(listOf(wrapperA), registry.wrappers(handlerA, original, tokenA))
        assertEquals(listOf(wrapperB), registry.wrappers(handlerB, original, tokenB))
        assertTrue(registry.wrappers(handlerA, original, tokenB).isEmpty())
        assertTrue(dropped.isEmpty())
    }

    @Test
    fun unregisterByWrapperOnlyRemovesMatchingOriginalWrapper() {
        val registry = HandlerWrapperRegistry { }
        val handlerA = Handler()
        val handlerB = Handler()
        val original = Runnable {}
        val wrapperA = Runnable {}
        val wrapperB = Runnable {}

        assertTrue(registry.register(handlerA, original, null, wrapperA, maxEntries = 10, maxWrappers = 10))
        assertTrue(registry.register(handlerB, original, null, wrapperB, maxEntries = 10, maxWrappers = 10))

        registry.unregister(original, wrapperA)

        assertTrue(registry.wrappers(handlerA, original, null).isEmpty())
        assertEquals(listOf(wrapperB), registry.wrappers(handlerB, original, null))
    }

    @Test
    fun unregisterHandlerTokenUsesHandlerScope() {
        val registry = HandlerWrapperRegistry { }
        val handlerA = Handler()
        val handlerB = Handler()
        val token = Any()
        val originalA = Runnable {}
        val originalB = Runnable {}
        val wrapperA = Runnable {}
        val wrapperB = Runnable {}

        assertTrue(registry.register(handlerA, originalA, token, wrapperA, maxEntries = 10, maxWrappers = 10))
        assertTrue(registry.register(handlerB, originalB, token, wrapperB, maxEntries = 10, maxWrappers = 10))

        registry.unregister(handlerA, token)

        assertTrue(registry.wrappers(handlerA, originalA, token).isEmpty())
        assertEquals(listOf(wrapperB), registry.wrappers(handlerB, originalB, token))
    }

    @Test
    fun nullTokenUnregisterRemovesAllWrappersForRunnable() {
        val registry = HandlerWrapperRegistry { }
        val handler = Handler()
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
        val dropped = mutableListOf<String>()
        val registry = HandlerWrapperRegistry(dropped::add)
        val handler = Handler()
        val original = Runnable {}
        val wrapper = Runnable {}

        assertTrue(registry.register(handler, original, null, wrapper, maxEntries = 1, maxWrappers = 1))
        assertFalse(registry.register(handler, original, null, Runnable {}, maxEntries = 1, maxWrappers = 1))
        assertFalse(registry.register(Handler(), Runnable {}, null, Runnable {}, maxEntries = 1, maxWrappers = 1))

        assertEquals(
            listOf(
                "jankhunter.handler_wrapper.dropped_wrappers.count",
                "jankhunter.handler_wrapper.dropped_entries.count",
            ),
            dropped,
        )
        assertSame(wrapper, registry.wrappers(handler, original, null).single())
    }
}
