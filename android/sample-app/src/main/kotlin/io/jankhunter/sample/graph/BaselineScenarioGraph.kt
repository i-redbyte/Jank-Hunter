package io.jankhunter.sample.graph

import io.jankhunter.runtime.JankHunterTelemetry

import android.os.SystemClock

internal class BaselineScenarioUseCase(
    private val repository: CheckoutBaselineRepository,
    private val observerRegistry: CheckoutObserverRegistry,
) {
    fun execute(): Long {
        var itemCount = 0L
        JankHunterTelemetry.withOwner(CheckoutBaselineDataSource::class.java.name) {
            itemCount = repository.loadItemCount()
        }
        return observerRegistry.publish(itemCount)
    }
}

internal class CheckoutBaselineRepository(
    private val dataSource: CheckoutBaselineDataSource,
) {
    fun loadItemCount(): Long {
        return dataSource.readItemCount()
    }
}

internal class CheckoutBaselineDataSource {
    fun readItemCount(): Long {
        SystemClock.sleep(8)
        return 24
    }
}

internal class CheckoutObserverRegistry {
    private val observer = CheckoutObserver(this)

    fun publish(itemCount: Long): Long {
        return observer.onCheckoutLoaded(itemCount)
    }

    fun acknowledge(itemCount: Long): Long {
        return itemCount
    }
}

internal class CheckoutObserver(
    private val registry: CheckoutObserverRegistry,
) {
    fun onCheckoutLoaded(itemCount: Long): Long {
        return registry.acknowledge(itemCount)
    }
}
