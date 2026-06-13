package io.jankhunter.gradle

import com.android.build.api.instrumentation.InstrumentationParameters
import org.gradle.api.provider.ListProperty
import org.gradle.api.provider.Property

interface JankHunterInstrumentationParameters : InstrumentationParameters {
    val methodCounters: Property<Boolean>
    val includePackages: ListProperty<String>
    val excludePackages: ListProperty<String>
}
