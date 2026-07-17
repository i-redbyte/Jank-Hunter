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
        val params = parameters.get()
        val classData = classContext.currentClassData
        val hierarchyResolver = LifecycleClassHierarchyResolver(classContext)
        return JankHunterClassVisitor(
            next = nextClassVisitor,
            className = classData.className,
            config = lifecycleHookConfig(params.instrumentationDiagnosticsDirectory.getOrElse("")),
            classHierarchy = hierarchyResolver.resolve(classData.className),
            resolveOwnerHierarchy = hierarchyResolver::resolve,
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
        val matched = InstrumentationMatcher(
            params.includePackages.getOrElse(emptySet()),
            params.excludePackages.getOrElse(emptySet()),
            params.includeWholeApplication.getOrElse(false),
        ).matches(classData.className)
        if (params.asmProgressLog.getOrElse(false)) {
            AsmProgressReporter.recordScanned(
                params.progressLabel.getOrElse("unknown:lifecycle"),
                classData.className,
                matched,
            )
        }
        return matched
    }

    private fun lifecycleHookConfig(diagnosticsDirectory: String): HookConfig {
        return HookConfig(
            embeddedSymbols = false,
            methodCounters = false,
            okhttp = false,
            webSockets = false,
            okHttpHelperAvailable = false,
            handlers = false,
            executors = false,
            coroutines = false,
            flowInteractions = false,
            lifecycleLeaks = true,
            logSpam = false,
            classGraph = false,
            runtimeCallGraph = false,
            classGraphDirectory = "",
            instrumentationDiagnosticsDirectory = diagnosticsDirectory,
            ownerMapEntriesDirectory = "",
        )
    }
}

private class LifecycleClassHierarchyResolver(
    private val classContext: ClassContext,
) {
    private val cache = mutableMapOf<String, Set<String>>()

    fun resolve(className: String): Set<String> {
        val root = className.replace('.', '/')
        return cache.getOrPut(root) {
            val resolved = linkedSetOf(root)
            val pending = ArrayDeque<String>()
            pending.add(root)
            while (pending.isNotEmpty()) {
                val candidate = pending.removeFirst()
                val data = runCatching {
                    classContext.loadClassData(candidate.replace('/', '.'))
                }.getOrNull() ?: continue
                (data.superClasses + data.interfaces).forEach { parent ->
                    val normalized = parent.replace('.', '/')
                    if (resolved.add(normalized)) pending.add(normalized)
                }
            }
            resolved
        }
    }
}
