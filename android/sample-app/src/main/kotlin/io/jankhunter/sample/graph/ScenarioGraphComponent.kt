package io.jankhunter.sample.graph

import io.jankhunter.sample.SampleApplication

internal class ScenarioGraphComponent(application: SampleApplication) {
    val baseline = BaselineScenarioUseCase(
        CheckoutBaselineRepository(CheckoutBaselineDataSource()),
        CheckoutObserverRegistry(),
    )
    val performance = PerformanceScenarioUseCase(
        CheckoutCalculator(),
        CheckoutRenderer(),
    )
    val network = NetworkScenarioUseCase(
        CheckoutNetworkRepository(CheckoutApi()),
    )
    val database = DatabaseScenarioUseCase(
        RoomDatabaseScenarioStore(SampleRoomDatabase.create(application)),
    )
    val memory = MemoryScenarioUseCase(
        CheckoutMemoryAllocator(application),
        CheckoutRetentionRepository(application),
        CheckoutAnalytics(),
    )
}
