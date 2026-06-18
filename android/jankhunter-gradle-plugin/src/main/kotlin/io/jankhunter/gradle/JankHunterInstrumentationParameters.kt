package io.jankhunter.gradle

import com.android.build.api.instrumentation.InstrumentationParameters
import org.gradle.api.provider.ListProperty
import org.gradle.api.provider.Property
import org.gradle.api.tasks.Input

interface JankHunterInstrumentationParameters : InstrumentationParameters {
    @get:Input
    val methodCounters: Property<Boolean>

    @get:Input
    val okhttp: Property<Boolean>

    @get:Input
    val webSockets: Property<Boolean>

    @get:Input
    val handlers: Property<Boolean>

    @get:Input
    val executors: Property<Boolean>

    @get:Input
    val coroutines: Property<Boolean>

    @get:Input
    val flowInteractions: Property<Boolean>

    @get:Input
    val logSpam: Property<Boolean>

    @get:Input
    val classGraph: Property<Boolean>

    @get:Input
    val runtimeCallGraph: Property<Boolean>

    @get:Input
    val classGraphPath: Property<String>

    @get:Input
    val instrumentationDiagnosticsPath: Property<String>

    @get:Input
    val ownerMapPath: Property<String>

    @get:Input
    val allowEmptyIncludePackages: Property<Boolean>

    @get:Input
    val asmProgressLog: Property<Boolean>

    @get:Input
    val progressLabel: Property<String>

    @get:Input
    val includePackages: ListProperty<String>

    @get:Input
    val excludePackages: ListProperty<String>
}
