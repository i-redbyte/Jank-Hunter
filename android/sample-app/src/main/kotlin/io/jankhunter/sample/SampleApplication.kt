package io.jankhunter.sample

import android.app.Application
import java.util.concurrent.CopyOnWriteArrayList

internal class SampleApplication : Application() {
    val retainedObjects = CopyOnWriteArrayList<Any>()
    val memoryPressure = CopyOnWriteArrayList<ByteArray>()

    override fun onCreate() {
        super.onCreate()
        LeakCanaryBridge.configureAutomatic()
    }

    fun resetScenario() {
        retainedObjects.clear()
        memoryPressure.clear()
    }
}

internal data class RetainedCheckoutScreen(
    val screen: Any,
    val payload: ByteArray,
)

internal data class RetainedCheckoutCache(
    val key: Int,
    val payload: ByteArray,
)

internal class ReleasedCheckoutProbe
