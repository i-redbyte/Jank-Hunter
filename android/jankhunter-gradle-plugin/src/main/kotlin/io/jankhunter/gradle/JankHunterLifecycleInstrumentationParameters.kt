package io.jankhunter.gradle

import com.android.build.api.instrumentation.InstrumentationParameters
import org.gradle.api.provider.Property
import org.gradle.api.provider.SetProperty
import org.gradle.api.tasks.Input

interface JankHunterLifecycleInstrumentationParameters : InstrumentationParameters {
    @get:Input
    val enabled: Property<Boolean>

    @get:Input
    val instrumentationDiagnosticsDirectory: Property<String>

    @get:Input
    val asmProgressLog: Property<Boolean>

    @get:Input
    val progressLabel: Property<String>

    @get:Input
    val includePackages: SetProperty<String>

    @get:Input
    val excludePackages: SetProperty<String>
}
