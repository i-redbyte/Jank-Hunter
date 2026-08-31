package io.jankhunter.runtime

internal class ContextTracker(
    initialScreen: String = "unknown",
) {
    private val screenOverride = ThreadLocal<String>()
    private val owner = ThreadLocal<String>()
    private val operation = ThreadLocal<JankHunterOperation>()
    private val propagatedOperationId = PrimitiveLongThreadLocal()

    @Volatile
    private var screen = initialScreen

    fun currentOwner(): String = owner.get() ?: "unknown"

    fun currentScreen(): String = screenOverride.get() ?: screen

    fun currentScreenOrNull(): String? = normalizedContextValue(screenOverride.get() ?: screen)

    fun ownerOrNull(): String? = owner.get()

    fun currentOperationId(): Long = currentOperationOrNull()?.id ?: propagatedOperationId.get()

    fun currentOperationOrNull(): JankHunterOperation? {
        var current = operation.get()
        if (current == null || !current.isFinished) return current
        val finishedHead = current
        do {
            current = current?.previousOperation
        } while (current?.isFinished == true)
        setThreadLocal(operation, current)
        detachFinishedChain(finishedHead, current)
        return current
    }

    fun activateOperation(value: JankHunterOperation) {
        operation.set(value)
    }

    fun deactivateOperation(value: JankHunterOperation) {
        if (operation.get() !== value) return
        var restored = value.previousOperation
        value.previousOperation = null
        while (restored != null && restored.isFinished) {
            val next = restored.previousOperation
            restored.previousOperation = null
            restored = next
        }
        setThreadLocal(operation, restored)
    }

    fun setScreen(screenName: String?) {
        screen = screenName?.takeIf { it.isNotEmpty() } ?: "unknown"
    }

    fun enterScopedContext(
        screenName: String?,
        ownerName: String?,
    ): JankHunterAnnotationScope {
        val token = JankHunterAnnotationScope(
            previousScreenOverride = screenOverride.get(),
            previousOwner = owner.get(),
        )
        normalizedContextValue(screenName)?.let { setThreadLocal(screenOverride, it) }
        normalizedContextValue(ownerName)?.let { setThreadLocal(owner, it) }
        return token
    }

    fun exitScopedContext(token: JankHunterAnnotationScope?) {
        if (token == null) return
        setThreadLocal(screenOverride, token.previousScreenOverride)
        setThreadLocal(owner, token.previousOwner)
    }

    fun capture(
        screenOverride: String? = null,
        ownerOverride: String? = null,
    ): JankHunterContext {
        return JankHunterContext(
            screen = normalizedContextValue(firstContextValue(screenOverride, currentScreen())),
            owner = normalizedContextValue(firstContextValue(ownerOverride, owner.get())),
            operationId = currentOperationId(),
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
        val previousOperation = operation.get()
        val previousPropagatedOperationId = propagatedOperationId.get()
        RuntimeHookGuard.run {
            setThreadLocal(screenOverride, context.screen)
            setThreadLocal(owner, normalizedContextValue(firstContextValue(ownerName, context.owner)))
            operation.remove()
            propagatedOperationId.set(context.operationId)
        }
        RuntimeHookGuard.run(onContextChanged)
        try {
            return block()
        } finally {
            RuntimeHookGuard.run { setThreadLocal(screenOverride, previousScreenOverride) }
            RuntimeHookGuard.run { setThreadLocal(owner, previousOwner) }
            RuntimeHookGuard.run { setThreadLocal(operation, previousOperation) }
            RuntimeHookGuard.run { propagatedOperationId.set(previousPropagatedOperationId) }
            RuntimeHookGuard.run(onContextChanged)
        }
    }

    private fun <T> setThreadLocal(target: ThreadLocal<T>, value: T?) {
        if (value == null) {
            target.remove()
        } else {
            target.set(value)
        }
    }

    private fun detachFinishedChain(
        head: JankHunterOperation,
        retained: JankHunterOperation?,
    ) {
        var current: JankHunterOperation? = head
        while (current != null && current !== retained) {
            val next = current.previousOperation
            current.previousOperation = null
            current = next
        }
    }
}

/** Reuses one mutable cell per participating thread instead of boxing every propagated ID. */
private class PrimitiveLongThreadLocal {
    private val local = ThreadLocal<Cell>()

    fun get(): Long = local.get()?.value ?: 0L

    fun set(value: Long) {
        if (value <= 0L) {
            local.remove()
            return
        }
        val cell = local.get()
        if (cell == null) {
            local.set(Cell(value))
        } else {
            cell.value = value
        }
    }

    private class Cell(var value: Long)
}

internal data class JankHunterContext(
    val screen: String?,
    val owner: String?,
    val operationId: Long = 0L,
)

internal fun firstContextValue(primary: String?, fallback: String?): String? {
    return normalizedContextValue(primary) ?: normalizedContextValue(fallback)
}

internal fun normalizedContextValue(value: String?): String? {
    val normalized = value?.trim()?.takeIf { it.isNotEmpty() }
    return normalized?.takeUnless { it == "unknown" }
}
