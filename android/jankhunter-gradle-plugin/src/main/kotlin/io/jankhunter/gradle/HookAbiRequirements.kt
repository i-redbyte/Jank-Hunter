package io.jankhunter.gradle

/** Maps enabled instrumentation to SDK hook owners validated before bytecode emission. */
internal object HookAbiRequirements {
    private const val HOOKS = "io/jankhunter/runtime/JankHunterHooks"
    private const val RUNTIME = "io/jankhunter/runtime/JankHunter"
    private const val IO = "io/jankhunter/runtime/JankHunterIOHooks"
    private const val ANDROID = "io/jankhunter/runtime/JankHunterAndroidHooks"
    private const val RESOURCES = "io/jankhunter/runtime/JankHunterResources"
    private const val BINDING = "io/jankhunter/runtime/JankHunterBindingAccessor"
    private const val LIFECYCLE_ACCESSOR = "io/jankhunter/runtime/JankHunterLifecycleAccessorV1"
    private const val LIFECYCLE_SINK = "io/jankhunter/runtime/JankHunterLifecycleTargetSinkV1"
    private const val EXECUTOR = "io/jankhunter/runtime/JankHunterTrackedExecutor"
    private const val HANDLER = "io/jankhunter/runtime/JankHunterTrackedHandler"
    private const val HTTP_ABI = "io/jankhunter/okhttp3/JankHunterHttpTransportV1"
    private const val OKHTTP = "io/jankhunter/okhttp3/JankHunterOkHttp3"
    private const val WORKER = "io/jankhunter/workmanager/JankHunterWorker"
    private const val COROUTINE_WORKER = "io/jankhunter/workmanager/JankHunterCoroutineWorker"

    fun requiredOwners(instrumentation: JankHunterResolvedInstrumentation, autoInit: Boolean): Set<String> {
        if (!instrumentation.hasRuntimeHooks && !autoInit) return emptySet()
        val owners = linkedSetOf<String>()
        if (autoInit) owners += RUNTIME
        if (instrumentation.hasRuntimeHooks) owners += HOOKS
        if (instrumentation.ioTracing) {
            owners += IO
            owners += RESOURCES
        }
        if (instrumentation.androidComponents || instrumentation.binderIPC) owners += ANDROID
        if (instrumentation.lifecycleLeaks) {
            owners += LIFECYCLE_ACCESSOR
            owners += LIFECYCLE_SINK
            owners += BINDING
        }
        if (instrumentation.handlers) owners += HANDLER
        if (instrumentation.executors) owners += EXECUTOR
        if (instrumentation.okhttp || instrumentation.webSockets) {
            owners += HTTP_ABI
            owners += OKHTTP
        }
        if (instrumentation.workerTracing) {
            owners += WORKER
            owners += COROUTINE_WORKER
        }
        return owners.intersect(HookAbiIndex.OWNERS)
    }

    fun scanOwners(required: Set<String>): Set<String> =
        if (required.isEmpty()) emptySet() else required + HOOKS
}
