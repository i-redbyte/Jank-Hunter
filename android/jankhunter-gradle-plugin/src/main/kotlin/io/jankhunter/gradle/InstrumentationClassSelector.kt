package io.jankhunter.gradle

import com.android.build.api.instrumentation.ClassData

internal data class InstrumentationClassSelection(
    val runtime: Boolean,
    val networkBoundary: Boolean,
    val databaseBoundary: Boolean,
    val autoInit: Boolean,
    val dependencyInjection: Boolean,
) {
    fun any(): Boolean = runtime || networkBoundary || databaseBoundary || autoInit || dependencyInjection
}

/** Evaluates all class-level instrumentation policies once for one immutable parameter snapshot. */
internal class InstrumentationClassSelector(
    private val params: JankHunterInstrumentationParameters,
) {
    fun evaluate(classData: ClassData): InstrumentationClassSelection {
        val alreadyInstrumented = InstrumentationMarker.isPresent(classData.classAnnotations)
        val generatedDiClass = DependencyInjectionClassMatcher.isGeneratedDiClass(classData)
        return InstrumentationClassSelection(
            runtime = runtimeMatches(classData, alreadyInstrumented, generatedDiClass),
            networkBoundary = networkMatches(classData, alreadyInstrumented),
            databaseBoundary = databaseMatches(classData, alreadyInstrumented),
            autoInit = autoInitMatches(classData, alreadyInstrumented),
            dependencyInjection = dependencyInjectionMatches(classData),
        )
    }

    private fun autoInitMatches(classData: ClassData, alreadyInstrumented: Boolean): Boolean {
        if (!params.autoInit.getOrElse(false) || alreadyInstrumented) return false
        if (classData.className.startsWith("io.jankhunter.runtime.")) return false
        return AndroidComponentAutoInit.matches(classData)
    }

    private fun runtimeMatches(
        classData: ClassData,
        alreadyInstrumented: Boolean,
        generatedDiClass: Boolean,
    ): Boolean {
        if (!runtimeHooksEnabled() || alreadyInstrumented || generatedDiClass) return false
        return InstrumentationMatcher(
            params.includePackages.getOrElse(emptySet()),
            params.excludePackages.getOrElse(emptySet()),
            params.includeWholeApplication.getOrElse(false),
        ).matches(classData.className)
    }

    private fun runtimeHooksEnabled(): Boolean {
        return params.methodCounters.getOrElse(false) ||
            params.okhttp.getOrElse(false) ||
            params.webSockets.getOrElse(false) ||
            params.handlers.getOrElse(false) ||
            params.executors.getOrElse(false) ||
            params.coroutines.getOrElse(false) ||
            params.interactionOperations.getOrElse(false) ||
            params.lifecycleLeaks.getOrElse(false) ||
            params.logSpam.getOrElse(false) ||
            params.classGraph.getOrElse(false) ||
            params.runtimeCallGraph.getOrElse(false) ||
            params.composeTracing.getOrElse(true) ||
            params.roomTracing.getOrElse(true) ||
            params.databaseTracing.getOrElse(true) ||
            params.workerTracing.getOrElse(true) ||
            params.androidComponents.getOrElse(false) ||
            params.binderIPC.getOrElse(false) ||
            params.ioTracing.getOrElse(false)
    }

    private fun dependencyInjectionMatches(classData: ClassData): Boolean {
        if (!params.dependencyInjectionAnalysis.getOrElse(false)) return false
        return DependencyInjectionClassMatcher.shouldScan(
            classData,
            params.includePackages.getOrElse(emptySet()),
            params.includeWholeApplication.getOrElse(false),
        )
    }

    private fun networkMatches(classData: ClassData, alreadyInstrumented: Boolean): Boolean {
        if (!params.networkWholeApplication.getOrElse(true) || alreadyInstrumented) return false
        if (!params.okhttp.getOrElse(false) && !params.webSockets.getOrElse(false)) return false
        return wholeApplicationBoundaryMatches(
            className = classData.className,
            excludePackages = params.excludePackages.getOrElse(emptySet()),
        )
    }

    private fun databaseMatches(classData: ClassData, alreadyInstrumented: Boolean): Boolean {
        if (!params.databaseWholeApplication.getOrElse(true) || alreadyInstrumented) return false
        if (!params.databaseTracing.getOrElse(true)) return false
        return databaseBoundaryMatches(
            className = classData.className,
            excludePackages = params.excludePackages.getOrElse(emptySet()),
        )
    }
}
