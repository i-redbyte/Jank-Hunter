package io.jankhunter.runtime

import java.util.concurrent.CopyOnWriteArraySet

internal sealed class RuntimeEvent {
    data class InitStatus(val diagnostics: JankHunterInitDiagnostics) : RuntimeEvent()
    data class InitFailure(val diagnostics: JankHunterInitDiagnostics) : RuntimeEvent()
    data class MetricsFlushed(val force: Boolean) : RuntimeEvent()
    data class LogSpamFlushed(val force: Boolean) : RuntimeEvent()
    data object ShutdownStarted : RuntimeEvent()
    data object ShutdownFinished : RuntimeEvent()
}

internal fun interface RuntimeObserver {
    fun onRuntimeEvent(event: RuntimeEvent)
}

internal class RuntimeEventBus {
    private val observers = CopyOnWriteArraySet<RuntimeObserver>()

    fun add(observer: RuntimeObserver) {
        observers.add(observer)
    }

    fun remove(observer: RuntimeObserver) {
        observers.remove(observer)
    }

    fun emit(event: RuntimeEvent) {
        observers.forEach { observer ->
            try {
                observer.onRuntimeEvent(event)
            } catch (_: Throwable) {
                // Observers are diagnostics hooks; they must never affect runtime collection.
            }
        }
    }
}
