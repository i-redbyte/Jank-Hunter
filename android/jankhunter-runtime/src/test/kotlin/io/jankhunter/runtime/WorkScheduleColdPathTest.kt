package io.jankhunter.runtime

import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class WorkScheduleColdPathTest {
    @Test fun builderObservationDoesNotConstructOrWaitForTheRuntimeGraph() {
        val facade = "io.jankhunter.runtime.JankHunterHooks"
        val rootName = "io.jankhunter.runtime.JankHunter"
        val graphName = "io.jankhunter.runtime.RuntimeComponentGraph"
        val parent = checkNotNull(javaClass.classLoader)
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val permitGraph = AtomicBoolean()
        val graphLoads = AtomicInteger()
        val loader = object : ClassLoader(parent) {
            override fun loadClass(name: String, resolve: Boolean): Class<*> {
                if (name == graphName) {
                    graphLoads.incrementAndGet()
                    check(permitGraph.get()) { "An inactive hook loaded the runtime graph" }
                    entered.countDown()
                    check(release.await(10, TimeUnit.SECONDS))
                }
                if (name != facade && name != rootName && !name.startsWith(rootName + "$")) {
                    return super.loadClass(name, resolve)
                }
                return (findLoadedClass(name) ?: run {
                    val bytes = checkNotNull(parent.getResourceAsStream(name.replace('.', '/') + ".class")).use { it.readBytes() }
                    defineClass(name, bytes, 0, bytes.size)
                }).also { if (resolve) resolveClass(it) }
            }
        }
        val hooks = loader.loadClass(facade)
        val period = hooks.getMethod("observeWorkBuilderPeriodCall", Any::class.java, java.lang.Long.TYPE,
            Any::class.java, java.lang.Long.TYPE, Any::class.java)
        period.invoke(null, Any(), 30L, TimeUnit.MINUTES, -1L, null)
        assertEquals("An inactive builder hook loaded the runtime graph", 0, graphLoads.get())
        val root = loader.loadClass(rootName)
        val observed = CountDownLatch(1)
        val failure = AtomicReference<Throwable?>()
        permitGraph.set(true)
        val startup = Thread {
            try { root.getMethod("isStarted").invoke(null) }
            catch (error: Throwable) { failure.set(error) }
        }
        val observer = Thread {
            try { period.invoke(null, Any(), 30L, TimeUnit.MINUTES, -1L, null) }
            catch (error: Throwable) { failure.set(error) }
            finally { observed.countDown() }
        }
        startup.start()
        try {
            assertTrue(entered.await(5, TimeUnit.SECONDS))
            observer.start()
            assertTrue("Metadata observation waited for SDK startup", observed.await(5, TimeUnit.SECONDS))
        } finally {
            release.countDown()
            startup.join(5_000)
            observer.join(5_000)
        }
        assertFalse(startup.isAlive)
        assertFalse(observer.isAlive)
        assertNull(failure.get())
    }
}
