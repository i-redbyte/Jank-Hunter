package io.jankhunter.okhttp3

import okhttp3.EventListener
import okhttp3.OkHttpClient
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterOkHttp3Test {
    @Test
    fun wrapEventListenerFactoryInstallsJankHunterFactoryWhenMissing() {
        assertTrue(JankHunterOkHttp3.wrapEventListenerFactory(null) is JankHunterEventListenerFactory)
    }

    @Test
    fun installEventListenerFactoryAddsJankHunterFactoryToBuilder() {
        val builder = OkHttpClient.Builder()

        val returned = JankHunterOkHttp3.installEventListenerFactory(builder)

        assertSame(builder, returned)
        assertTrue(eventListenerFactory(builder) is JankHunterEventListenerFactory)
    }

    @Test
    fun installEventListenerFactoryPreservesExistingFactoryAsDelegate() {
        val original = EventListener.Factory { EventListener.NONE }
        val builder = OkHttpClient.Builder().eventListenerFactory(original)

        JankHunterOkHttp3.installEventListenerFactory(builder)

        val factory = eventListenerFactory(builder)
        assertTrue(factory is JankHunterEventListenerFactory)
        assertSame(original, delegate(factory as JankHunterEventListenerFactory))
    }

    @Test
    fun installEventListenerFactoryDoesNotWrapTwice() {
        val builder = OkHttpClient.Builder()
        JankHunterOkHttp3.installEventListenerFactory(builder)
        val first = eventListenerFactory(builder)

        JankHunterOkHttp3.installEventListenerFactory(builder)

        assertSame(first, eventListenerFactory(builder))
    }

    private fun eventListenerFactory(builder: OkHttpClient.Builder): EventListener.Factory {
        val field = builder.javaClass.getDeclaredField("eventListenerFactory")
        field.isAccessible = true
        return field.get(builder) as EventListener.Factory
    }

    private fun delegate(factory: JankHunterEventListenerFactory): EventListener.Factory? {
        val field = JankHunterEventListenerFactory::class.java.getDeclaredField("delegate")
        field.isAccessible = true
        return field.get(factory) as? EventListener.Factory
    }
}
