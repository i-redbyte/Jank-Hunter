package io.jankhunter.gradle

import com.android.build.api.instrumentation.InstrumentationParameters
import org.gradle.api.provider.Property
import org.gradle.api.provider.SetProperty
import org.gradle.api.tasks.Input

interface JankHunterInstrumentationParameters : InstrumentationParameters {
    @get:Input
    val autoInit: Property<Boolean>

    @get:Input
    val dependencyInjectionAnalysis: Property<Boolean>

    @get:Input
    val methodCounters: Property<Boolean>

    @get:Input
    val methodFilterMode: Property<JankHunterMethodFilterMode>

    @get:Input
    val okhttp: Property<Boolean>

    @get:Input
    val webSockets: Property<Boolean>

    @get:Input
    val okHttpHelperAvailable: Property<Boolean>

    @get:Input
    val handlers: Property<Boolean>

    @get:Input
    val executors: Property<Boolean>

    @get:Input
    val coroutines: Property<Boolean>

    @get:Input
    val interactionOperations: Property<Boolean>

    @get:Input
    val lifecycleLeaks: Property<Boolean>

    @get:Input
    val logSpam: Property<Boolean>

    @get:Input
    val classGraph: Property<Boolean>

    @get:Input
    val runtimeCallGraph: Property<Boolean>

    @get:Input
    val composeTracing: Property<Boolean>

    @get:Input
    val roomTracing: Property<Boolean>

    @get:Input
    val databaseTracing: Property<Boolean>

    @get:Input
    val workerTracing: Property<Boolean>

    @get:Input
    val androidComponents: Property<Boolean>

    @get:Input
    val binderIPC: Property<Boolean>

    @get:Input
    val ioTracing: Property<Boolean>

    @get:Input
    val classGraphDirectory: Property<String>

    @get:Input
    val instrumentationDiagnosticsDirectory: Property<String>

    @get:Input
    val androidComponentCatalogDirectory: Property<String>

    @get:Input
    val dependencyInjectionCatalogDirectory: Property<String>

    @get:Input
    val includeWholeApplication: Property<Boolean>

    @get:Input
    val networkWholeApplication: Property<Boolean>

    @get:Input
    val databaseWholeApplication: Property<Boolean>

    @get:Input
    val includePackages: SetProperty<String>

    @get:Input
    val excludePackages: SetProperty<String>
}
