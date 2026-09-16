package io.jankhunter.runtime.integration

import androidx.metrics.performance.FrameData
import androidx.metrics.performance.JankStats
import io.jankhunter.runtime.RuntimeHookFailureReason
import io.jankhunter.runtime.RuntimeHookFailureTracker
import java.lang.reflect.Proxy
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class JankStatsFrameCallbackTest {
    @Test
    fun forwardsPrimitivesInOrderOnBothCallbackThreadsWithoutRetainingFrameData() {
        val observed = mutableListOf<Pair<Boolean, Long>>()
        val callback = checkNotNull(JankHunterJankStats.createFrameListener { jank, nanos -> observed.add(jank to nanos) })
        val listener = callback as JankStats.OnFrameListener
        val frame = FrameData(1L, 16_000_000L, false, emptyList())
        listener.onFrame(frame)
        val worker = Executors.newSingleThreadExecutor()
        try {
            worker.submit { listener.onFrame(FrameData(2L, 24_000_000L, true, emptyList())) }.get(2, TimeUnit.SECONDS)
        } finally {
            worker.shutdownNow()
            assertTrue(worker.awaitTermination(2, TimeUnit.SECONDS))
        }
        assertEquals(listOf(false to 16_000_000L, true to 24_000_000L), observed)
        assertFalse(callback.javaClass.declaredFields.any { field ->
            field.isAccessible = true
            field.get(callback) === frame
        })
    }

    @Test
    fun uninstallReleasesCallbackAndDoesNotDeliverLaterFrames() {
        var calls = 0
        val callback = JankStatsFrameCallback { _, _ -> calls++ }
        val handle = JankHunterJankStats.Handle(object : JankHunterJankStats.TrackingControl {
            override fun setTrackingEnabled(enabled: Boolean) = Unit
            override fun close() = callback.close()
        })
        val frame = FrameData(1L, 16L, false, emptyList())
        callback.onFrame(frame)
        handle.uninstall()
        callback.onFrame(frame)
        handle.setTrackingEnabled(true)
        callback.onFrame(frame)
        assertEquals(1, calls)
        assertTrue(callback.javaClass.declaredFields.filterNot { it.type.isPrimitive }.all { field ->
            field.isAccessible = true
            field.get(callback) == null
        })
    }

    @Test
    fun frameFailuresAreCountedAndFatalErrorsPropagate() {
        val before = RuntimeHookFailureTracker.count(RuntimeHookFailureReason.JANKSTATS_FRAME)
        val callback = JankStatsFrameCallback { _, _ -> error("application callback") }
        callback.onFrame(FrameData(1L, 16L, false, emptyList()))
        assertEquals(before + 1L, RuntimeHookFailureTracker.count(RuntimeHookFailureReason.JANKSTATS_FRAME))
        val fatal = JankHunterJankStatsTest.FatalTestError()
        val fatalCallback = JankStatsFrameCallback { _, _ -> throw fatal }
        assertEquals(fatal, assertThrows(JankHunterJankStatsTest.FatalTestError::class.java) {
            fatalCallback.onFrame(FrameData(1L, 16L, false, emptyList()))
        })
    }

    @Test
    fun optionalFacadeLoadsWithoutAnyAndroidXMetricsClasses() {
        val isolated = WithoutMetricsLoader(checkNotNull(javaClass.classLoader))
        val facade = isolated.loadClass("io.jankhunter.runtime.integration.JankHunterJankStats")
        val frameListener = isolated.loadClass("io.jankhunter.runtime.integration.JankHunterJankStats\$FrameListener")
        val callback = Proxy.newProxyInstance(isolated, arrayOf(frameListener)) { _, _, _ -> null }
        val create = facade.declaredMethods.single { it.name.startsWith("createFrameListener") }
        assertNull(create.invoke(facade.getField("INSTANCE").get(null), callback))
    }

    private class WithoutMetricsLoader(parent: ClassLoader) : ClassLoader(parent) {
        override fun loadClass(name: String, resolve: Boolean): Class<*> {
            synchronized(this) {
                if (name.startsWith("androidx.metrics.")) throw ClassNotFoundException(name)
                if (!name.startsWith("io.jankhunter.runtime.")) return super.loadClass(name, resolve)
                val loaded = findLoadedClass(name) ?: run {
                    val resource = name.replace('.', '/') + ".class"
                    val bytes = checkNotNull(parent.getResourceAsStream(resource)).use { it.readBytes() }
                    defineClass(name, bytes, 0, bytes.size)
                }
                if (resolve) resolveClass(loaded)
                return loaded
            }
        }
    }
}
