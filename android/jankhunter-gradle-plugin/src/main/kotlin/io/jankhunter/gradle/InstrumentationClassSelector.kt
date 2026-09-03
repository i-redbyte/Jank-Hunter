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

/** Allocation-free class-level policy over the normalized, immutable AGP parameter values. */
internal object InstrumentationClassSelector {
    fun evaluate(
        params: JankHunterInstrumentationParameters,
        classData: ClassData,
    ): InstrumentationClassSelection {
        val includePackages = params.includePackages.getOrElse(emptySet())
        val excludePackages = params.excludePackages.getOrElse(emptySet())
        val includeWholeApplication = params.includeWholeApplication.getOrElse(false)
        val okhttpEnabled = params.okhttp.getOrElse(false)
        val webSocketsEnabled = params.webSockets.getOrElse(false)
        val databaseTracingEnabled = params.databaseTracing.getOrElse(true)
        val runtimeHooksEnabled = params.methodCounters.getOrElse(false) ||
            okhttpEnabled ||
            webSocketsEnabled ||
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
            databaseTracingEnabled ||
            params.workerTracing.getOrElse(true) ||
            params.androidComponents.getOrElse(false) ||
            params.binderIPC.getOrElse(false) ||
            params.ioTracing.getOrElse(false)
        val networkBoundaryEnabled = params.networkWholeApplication.getOrElse(true) &&
            (okhttpEnabled || webSocketsEnabled)
        val databaseBoundaryEnabled = params.databaseWholeApplication.getOrElse(true) &&
            databaseTracingEnabled
        val alreadyInstrumented = InstrumentationMarker.isPresent(classData.classAnnotations)
        val generatedDiClass = DependencyInjectionClassMatcher.isGeneratedDiClass(classData)
        val normalizedClassName = InstrumentationPackages.normalizePackage(classData.className)
        val boundaryMatches = !alreadyInstrumented && (networkBoundaryEnabled || databaseBoundaryEnabled) &&
            InstrumentationMatcher.matchesNormalizedClassName(
                normalizedClassName,
                emptySet(),
                excludePackages,
                includeWholeApplication = true,
            )
        return InstrumentationClassSelection(
            runtime = runtimeHooksEnabled && !alreadyInstrumented && !generatedDiClass &&
                InstrumentationMatcher.matchesNormalizedClassName(
                    normalizedClassName,
                    includePackages,
                    excludePackages,
                    includeWholeApplication,
                ),
            networkBoundary = networkBoundaryEnabled && boundaryMatches,
            databaseBoundary = databaseBoundaryEnabled && boundaryMatches &&
                !isDatabaseImplementationClass(normalizedClassName),
            autoInit = params.autoInit.getOrElse(false) && !alreadyInstrumented &&
                !normalizedClassName.startsWith(JANK_HUNTER_RUNTIME_PACKAGE) &&
                AndroidComponentAutoInit.matches(classData),
            dependencyInjection = params.dependencyInjectionAnalysis.getOrElse(false) &&
                (generatedDiClass || dependencyInjectionMatches(
                    normalizedClassName,
                    includePackages,
                    includeWholeApplication,
                )),
        )
    }

    private fun dependencyInjectionMatches(
        normalizedClassName: String,
        includePackages: Set<String>,
        includeWholeApplication: Boolean,
    ): Boolean {
        return if (includeWholeApplication) {
            InstrumentationMatcher.matchesNormalizedClassName(
                normalizedClassName,
                emptySet(),
                emptySet(),
                includeWholeApplication = true,
            )
        } else {
            InstrumentationMatcher.matchesNormalizedClassName(
                normalizedClassName,
                includePackages,
                JANK_HUNTER_PACKAGES,
                includeBuiltinExcludes = false,
            )
        }
    }

    private const val JANK_HUNTER_RUNTIME_PACKAGE = "io.jankhunter.runtime."
    private val JANK_HUNTER_PACKAGES = setOf("io.jankhunter")
}
