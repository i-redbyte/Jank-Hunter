package io.jankhunter.gradle

import org.gradle.api.Action

open class JankHunterExtension {
    val enabledBuildTypes: MutableSet<String> = linkedSetOf("debug")
    var autoInit: Boolean = true
    val instrument: Instrumentation = Instrumentation()

    fun instrument(action: Action<Instrumentation>) {
        action.execute(instrument)
    }

    open class Instrumentation {
        var activities: Boolean = true
        var fragments: Boolean = true
        var okhttp: Boolean = true
        var webSockets: Boolean = true
        var handlers: Boolean = true
        var executors: Boolean = true
        var rxJava: Boolean = true
        var coroutines: Boolean = false
        var methodCounters: Boolean = false
        var allowEmptyIncludePackages: Boolean = false
        val includePackages: MutableSet<String> = linkedSetOf()
        val excludePackages: MutableSet<String> = linkedSetOf()
    }
}
