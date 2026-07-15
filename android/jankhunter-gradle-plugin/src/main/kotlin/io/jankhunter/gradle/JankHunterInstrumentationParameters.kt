package io.jankhunter.gradle

import com.android.build.api.instrumentation.InstrumentationParameters
import org.gradle.api.provider.Property
import org.gradle.api.provider.SetProperty
import org.gradle.api.tasks.Input

interface JankHunterInstrumentationParameters : InstrumentationParameters {
    @get:Input
    val embeddedSymbols: Property<Boolean>

    @get:Input
    val dependencyInjectionAnalysis: Property<Boolean>

    @get:Input
    val methodCounters: Property<Boolean>

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
    val flowInteractions: Property<Boolean>

    @get:Input
    val lifecycleLeaks: Property<Boolean>

    @get:Input
    val logSpam: Property<Boolean>

    @get:Input
    val classGraph: Property<Boolean>

    @get:Input
    val runtimeCallGraph: Property<Boolean>

    @get:Input
    val classGraphDirectory: Property<String>

    @get:Input
    val instrumentationDiagnosticsDirectory: Property<String>

    @get:Input
    val ownerMapEntriesDirectory: Property<String>

    @get:Input
    val dependencyInjectionCatalogDirectory: Property<String>

    @get:Input
    val asmProgressLog: Property<Boolean>

    @get:Input
    val progressLabel: Property<String>

    @get:Input
    val includePackages: SetProperty<String>

    @get:Input
    val excludePackages: SetProperty<String>
}
