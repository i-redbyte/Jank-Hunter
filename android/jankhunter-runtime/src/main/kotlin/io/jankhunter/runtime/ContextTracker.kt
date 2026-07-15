package io.jankhunter.runtime

internal class ContextTracker(
    initialScreen: String = "unknown",
) {
    private val screenOverride = ThreadLocal<String>()
    private val owner = ThreadLocal<String>()
    private val flow = ThreadLocal<String>()
    private val flowStep = ThreadLocal<String>()
    private val lock = Any()

    @Volatile
    private var screen = initialScreen

    private var lastRecordedContext: JankHunterContext? = null

    fun currentOwner(): String = owner.get() ?: "unknown"

    fun currentScreen(): String = screenOverride.get() ?: screen

    fun currentFlow(): String = flow.get() ?: "unknown"

    fun currentFlowStep(): String = flowStep.get() ?: "unknown"

    fun ownerOrNull(): String? = owner.get()

    fun setScreen(screenName: String?) {
        screen = screenName?.takeIf { it.isNotEmpty() } ?: "unknown"
    }

    fun startFlow(flowName: String?): JankHunterFlow {
        val token = JankHunterFlow(
            previousFlow = flow.get(),
            previousStep = flowStep.get(),
        )
        setThreadLocal(flow, normalizedContextValue(flowName))
        flowStep.remove()
        return token
    }

    fun endFlow(token: JankHunterFlow?) {
        if (token == null) return
        setThreadLocal(flow, token.previousFlow)
        setThreadLocal(flowStep, token.previousStep)
    }

    fun markFlowStep(stepName: String?) {
        setThreadLocal(flowStep, normalizedContextValue(stepName))
    }

    fun enterScopedContext(
        screenName: String?,
        ownerName: String?,
        flowName: String?,
        stepName: String?,
    ): JankHunterAnnotationScope {
        val token = JankHunterAnnotationScope(
            previousScreenOverride = screenOverride.get(),
            previousOwner = owner.get(),
            previousFlow = flow.get(),
            previousStep = flowStep.get(),
        )
        normalizedContextValue(screenName)?.let { setThreadLocal(screenOverride, it) }
        normalizedContextValue(ownerName)?.let { setThreadLocal(owner, it) }
        normalizedContextValue(flowName)?.let {
            setThreadLocal(flow, it)
            flowStep.remove()
        }
        normalizedContextValue(stepName)?.let { setThreadLocal(flowStep, it) }
        return token
    }

    fun exitScopedContext(token: JankHunterAnnotationScope?) {
        if (token == null) return
        setThreadLocal(screenOverride, token.previousScreenOverride)
        setThreadLocal(owner, token.previousOwner)
        setThreadLocal(flow, token.previousFlow)
        setThreadLocal(flowStep, token.previousStep)
    }

    fun capture(
        screenOverride: String? = null,
        ownerOverride: String? = null,
    ): JankHunterContext {
        return JankHunterContext(
            screen = normalizedContextValue(firstContextValue(screenOverride, currentScreen())),
            owner = normalizedContextValue(firstContextValue(ownerOverride, owner.get())),
            flow = normalizedContextValue(flow.get()),
            step = normalizedContextValue(flowStep.get()),
        )
    }

    fun <T> callWithContext(
        context: JankHunterContext,
        ownerName: String?,
        onContextChanged: () -> Unit,
        block: () -> T,
    ): T {
        val previousScreenOverride = screenOverride.get()
        val previousOwner = owner.get()
        val previousFlow = flow.get()
        val previousStep = flowStep.get()
        RuntimeHookGuard.run {
            setThreadLocal(screenOverride, context.screen)
            setThreadLocal(owner, normalizedContextValue(firstContextValue(ownerName, context.owner)))
            setThreadLocal(flow, context.flow)
            setThreadLocal(flowStep, context.step)
        }
        RuntimeHookGuard.run(onContextChanged)
        try {
            return block()
        } finally {
            RuntimeHookGuard.run { setThreadLocal(screenOverride, previousScreenOverride) }
            RuntimeHookGuard.run { setThreadLocal(owner, previousOwner) }
            RuntimeHookGuard.run { setThreadLocal(flow, previousFlow) }
            RuntimeHookGuard.run { setThreadLocal(flowStep, previousStep) }
            RuntimeHookGuard.run(onContextChanged)
        }
    }

    fun shouldRecord(tuple: JankHunterContext): Boolean {
        synchronized(lock) {
            if (tuple == lastRecordedContext) return false
            lastRecordedContext = tuple
            return true
        }
    }

    fun resetRecordedContext() {
        synchronized(lock) {
            lastRecordedContext = null
        }
    }

    private fun <T> setThreadLocal(target: ThreadLocal<T>, value: T?) {
        if (value == null) {
            target.remove()
        } else {
            target.set(value)
        }
    }
}

internal data class JankHunterContext(
    val screen: String?,
    val owner: String?,
    val flow: String?,
    val step: String?,
)

internal fun firstContextValue(primary: String?, fallback: String?): String? {
    return normalizedContextValue(primary) ?: normalizedContextValue(fallback)
}

internal fun normalizedContextValue(value: String?): String? {
    val normalized = value?.trim()?.takeIf { it.isNotEmpty() }
    return normalized?.takeUnless { it == "unknown" }
}
