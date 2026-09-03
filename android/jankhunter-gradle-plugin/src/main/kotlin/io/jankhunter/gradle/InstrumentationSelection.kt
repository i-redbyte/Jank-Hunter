package io.jankhunter.gradle

import com.android.build.api.instrumentation.ClassContext
import com.android.build.api.instrumentation.ClassData
import org.objectweb.asm.Opcodes

internal fun networkBoundaryMatches(className: String, excludePackages: Iterable<String>): Boolean {
    return wholeApplicationBoundaryMatches(className, excludePackages)
}

internal fun databaseBoundaryMatches(className: String, excludePackages: Iterable<String>): Boolean {
    if (isDatabaseImplementationClass(className)) return false
    return wholeApplicationBoundaryMatches(className, excludePackages)
}

internal fun wholeApplicationBoundaryMatches(className: String, excludePackages: Iterable<String>): Boolean {
    val normalized = InstrumentationPackages.normalizePackage(className)
    if (InstrumentationPackages.isBuiltinExcluded(normalized)) return false
    if (InstrumentationPackages.isGeneratedAndroidClass(normalized)) return false
    for (excluded in excludePackages) {
        val boundary = InstrumentationPackages.normalizePackage(excluded)
        if (boundary.isNotEmpty() && (normalized == boundary || normalized.startsWith("$boundary."))) {
            return false
        }
    }
    return true
}

internal object InstrumentationMarker {
    const val DESCRIPTOR = "Lio/jankhunter/runtime/JankHunterInstrumented;"
    private const val CLASS_NAME = "io.jankhunter.runtime.JankHunterInstrumented"

    fun isPresent(annotations: Iterable<String>): Boolean {
        return hasInstrumentationMarker(annotations, DESCRIPTOR, CLASS_NAME)
    }
}

internal object LifecycleInstrumentationMarker {
    const val DESCRIPTOR = "Lio/jankhunter/runtime/JankHunterLifecycleInstrumented;"
    private const val CLASS_NAME = "io.jankhunter.runtime.JankHunterLifecycleInstrumented"

    fun isPresent(annotations: Iterable<String>): Boolean {
        return hasInstrumentationMarker(annotations, DESCRIPTOR, CLASS_NAME)
    }
}

private fun hasInstrumentationMarker(
    annotations: Iterable<String>,
    descriptor: String,
    className: String,
): Boolean {
    return annotations.any { annotation ->
        annotation == descriptor || annotation.replace('/', '.').removePrefix("L").removeSuffix(";") == className
    }
}

/** One instance is confined to one ASM class visitor because AGP scopes [ClassContext] to that class. */
internal class ClassHierarchyResolver(
    private val classContext: ClassContext,
) {
    private val cache = mutableMapOf<String, Set<String>>()
    private var failures: MutableMap<ClassHierarchyResolutionFailure, Int>? = null
    private var omittedFailures = 0

    fun resolve(className: String): Set<String> {
        val root = className.toInternalClassName()
        return cache.getOrPut(root) {
            val resolved = linkedSetOf(root)
            val pending = ArrayDeque<String>()
            pending.add(root)
            while (pending.isNotEmpty()) {
                val candidate = pending.removeFirst()
                val data = try {
                    classContext.loadClassData(candidate.replace('/', '.'))
                } catch (exception: Exception) {
                    if (exception is InterruptedException || exception is java.util.concurrent.CancellationException) {
                        throw exception
                    }
                    recordFailure(candidate, exception)
                    continue
                } ?: continue
                (data.superClasses + data.interfaces).forEach { parent ->
                    val normalized = parent.toInternalClassName()
                    if (resolved.add(normalized)) pending.add(normalized)
                }
            }
            resolved
        }
    }

    fun diagnostics(): Map<ClassHierarchyResolutionFailure, Int> {
        val recorded = failures ?: return emptyMap()
        if (omittedFailures == 0) return recorded
        return LinkedHashMap(recorded).apply {
            put(
                ClassHierarchyResolutionFailure(
                    className = classContext.currentClassData.className,
                    errorType = CLASS_HIERARCHY_OMITTED_FAILURE_TYPE,
                    detail = null,
                ),
                omittedFailures,
            )
        }
    }

    private fun recordFailure(className: String, exception: Exception) {
        val failure = ClassHierarchyResolutionFailure(
            className = className.replace('/', '.'),
            errorType = exception.javaClass.name,
            detail = exception.message?.take(MAX_DETAIL_LENGTH),
        )
        val diagnostics = failures ?: linkedMapOf<ClassHierarchyResolutionFailure, Int>().also { failures = it }
        val count = diagnostics[failure]
        when {
            count != null -> diagnostics[failure] = count + 1
            diagnostics.size < MAX_RECORDED_FAILURES -> diagnostics[failure] = 1
            else -> omittedFailures++
        }
    }

    private fun String.toInternalClassName(): String = replace('.', '/')

    private companion object {
        const val MAX_RECORDED_FAILURES = 32
        const val MAX_DETAIL_LENGTH = 256
    }
}

internal const val CLASS_HIERARCHY_OMITTED_FAILURE_TYPE = "additional_failures_omitted"

internal data class ClassHierarchyResolutionFailure(
    val className: String,
    val errorType: String,
    val detail: String?,
)

internal data class HookConfig(
    val autoInit: Boolean = false,
    val methodCounters: Boolean,
    val methodFilterMode: JankHunterMethodFilterMode = JankHunterMethodFilterMode.FILTER,
    val okhttp: Boolean,
    val webSockets: Boolean,
    val okHttpHelperAvailable: Boolean = true,
    val handlers: Boolean,
    val executors: Boolean,
    val coroutines: Boolean,
    val interactionOperations: Boolean,
    val logSpam: Boolean,
    val classGraph: Boolean,
    val runtimeCallGraph: Boolean,
    val classGraphDirectory: String,
    val instrumentationDiagnosticsDirectory: String,
    val androidComponentCatalogDirectory: String = "",
    val androidComponents: Boolean = false,
    val binderIPC: Boolean = false,
    val lifecycleLeaks: Boolean = false,
    val composeTracing: Boolean = true,
    val roomTracing: Boolean = true,
    val databaseTracing: Boolean = true,
    val workerTracing: Boolean = true,
    val ioTracing: Boolean = false,
) {
    fun autoInitOnly(): HookConfig = copy(
        methodCounters = false,
        okhttp = false,
        webSockets = false,
        handlers = false,
        executors = false,
        coroutines = false,
        interactionOperations = false,
        logSpam = false,
        classGraph = false,
        runtimeCallGraph = false,
        androidComponents = false,
        binderIPC = false,
        lifecycleLeaks = false,
        composeTracing = false,
        roomTracing = false,
        databaseTracing = false,
        workerTracing = false,
        ioTracing = false,
    )

    fun boundaryOnly(network: Boolean, database: Boolean): HookConfig = copy(
        autoInit = false,
        methodCounters = false,
        okhttp = okhttp && network,
        webSockets = webSockets && network,
        handlers = false,
        executors = false,
        coroutines = false,
        interactionOperations = false,
        logSpam = false,
        classGraph = false,
        runtimeCallGraph = false,
        androidComponents = false,
        binderIPC = false,
        lifecycleLeaks = false,
        composeTracing = false,
        roomTracing = false,
        databaseTracing = databaseTracing && database,
        workerTracing = false,
        ioTracing = false,
    )
}

internal object AndroidComponentAutoInit {
    private val componentBases = setOf(
        "android.app.Application",
        "android.app.Activity",
        "android.app.Service",
        "android.content.ContentProvider",
        "android.content.BroadcastReceiver",
    )

    fun matches(classData: ClassData): Boolean {
        return classData.superClasses.any { it.replace('/', '.') in componentBases }
    }
}

internal enum class AutoInitComponent(
    val methodName: String,
    val methodDescriptor: String,
    val syntheticAccess: Int?,
) {
    APPLICATION("onCreate", "()V", Opcodes.ACC_PUBLIC),
    ACTIVITY("onCreate", "(Landroid/os/Bundle;)V", Opcodes.ACC_PROTECTED),
    SERVICE("onCreate", "()V", Opcodes.ACC_PUBLIC),
    CONTENT_PROVIDER("onCreate", "()Z", null),
    BROADCAST_RECEIVER(
        "onReceive",
        "(Landroid/content/Context;Landroid/content/Intent;)V",
        null,
    ),
    ;

    fun matches(name: String, descriptor: String): Boolean {
        return name == methodName && descriptor == methodDescriptor
    }

    companion object {
        fun fromHierarchy(hierarchy: Set<String>): AutoInitComponent? {
            return when {
                "android/app/Application" in hierarchy -> APPLICATION
                "android/app/Activity" in hierarchy -> ACTIVITY
                "android/app/Service" in hierarchy -> SERVICE
                "android/content/ContentProvider" in hierarchy -> CONTENT_PROVIDER
                "android/content/BroadcastReceiver" in hierarchy -> BROADCAST_RECEIVER
                else -> null
            }
        }
    }
}
