package io.jankhunter.gradle

import com.android.build.api.instrumentation.AsmClassVisitorFactory
import com.android.build.api.instrumentation.ClassContext
import com.android.build.api.instrumentation.ClassData
import org.objectweb.asm.ClassVisitor

/**
 * Extends leak coverage to internal dependency modules without enabling high-volume runtime hooks
 * such as method counters or the runtime call graph outside the application module.
 */
abstract class JankHunterLifecycleClassVisitorFactory :
    AsmClassVisitorFactory<JankHunterLifecycleInstrumentationParameters> {
    override fun createClassVisitor(
        classContext: ClassContext,
        nextClassVisitor: ClassVisitor,
    ): ClassVisitor {
        val classData = classContext.currentClassData
        val hierarchyResolver = ClassHierarchyResolver(classContext)
        return JankHunterClassVisitor(
            next = nextClassVisitor,
            className = classData.className,
            config = lifecycleHookConfig(
                parameters.get().instrumentationDiagnosticsDirectory.getOrElse(""),
            ),
            classHierarchy = hierarchyResolver.resolve(classData.className),
            resolveOwnerHierarchy = hierarchyResolver::resolve,
            hierarchyResolutionDiagnostics = hierarchyResolver::diagnostics,
            instrumentationMarkerDescriptor = LifecycleInstrumentationMarker.DESCRIPTOR,
            markerOnlyWhenHookApplied = true,
            diagnosticsOnlyWhenHookApplied = true,
        )
    }

    override fun isInstrumentable(classData: ClassData): Boolean {
        val params = parameters.get()
        if (!params.enabled.getOrElse(false)) return false
        if (LifecycleInstrumentationMarker.isPresent(classData.classAnnotations)) return false
        if (DependencyInjectionClassMatcher.isGeneratedDiClass(classData)) return false
        return InstrumentationMatcher.matchesNormalizedClassName(
            InstrumentationPackages.normalizePackage(classData.className),
            params.includePackages.getOrElse(emptySet()),
            params.excludePackages.getOrElse(emptySet()),
            params.includeWholeApplication.getOrElse(false),
        )
    }
}

internal fun lifecycleHookConfig(diagnosticsDirectory: String): HookConfig {
    return HookConfig(
        methodCounters = false,
        okhttp = false,
        webSockets = false,
        okHttpHelperAvailable = false,
        handlers = false,
        executors = false,
        coroutines = false,
        interactionOperations = false,
        lifecycleLeaks = true,
        logSpam = false,
        classGraph = false,
        runtimeCallGraph = false,
        composeTracing = false,
        roomTracing = false,
        databaseTracing = false,
        workerTracing = false,
        ioTracing = false,
        classGraphDirectory = "",
        instrumentationDiagnosticsDirectory = diagnosticsDirectory,
    )
}
