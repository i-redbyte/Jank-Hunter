package io.jankhunter.runtime

internal class ContextTracker(
    initialScreen: String = "unknown",
) {
    private val screenOverride = ThreadLocal<String>()
    private val owner = ThreadLocal<String>()
    private val operation = ThreadLocal<JankHunterOperation>()
    private val propagatedOperationId = PrimitiveLongThreadLocal()
    private val capturedContext = ThreadLocal<CapturedContextCell>()

    @Volatile
    private var screen = initialScreen

    fun currentOwner(): String = owner.get() ?: "unknown"

    fun currentScreen(): String = screenOverride.get() ?: screen

    fun currentScreenOrNull(): String? = capturedScreen(null)

    fun ownerOrNull(): String? = owner.get()

    fun currentOperationId(): Long = currentOperationOrNull()?.id ?: propagatedOperationId.get()

    fun currentOperationName(): String? = currentOperationOrNull()?.name

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
        val screen = capturedScreen(screenOverride)
        val owner = capturedOwner(ownerOverride)
        val operationId = currentOperationId()
        val cell = capturedContext.get()
        val current = cell?.value
        if (current != null && current.matches(screen, owner, operationId)) return current

        val created = JankHunterContext(screen, owner, operationId)
        if (cell == null) capturedContext.set(CapturedContextCell(created)) else cell.value = created
        return created
    }

    fun capturedScreen(override: String?): String? {
        return normalizedContextValue(firstContextValue(override, currentScreen()))
    }

    fun capturedOwner(override: String?): String? {
        return normalizedContextValue(firstContextValue(override, owner.get()))
    }

    inline fun <T> callWithContext(
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
            // Null in a captured snapshot means unknown, not a later global screen.
            screenOverride.set(context.screen ?: "unknown")
            owner.set(normalizedContextValue(firstContextValue(ownerName, context.owner)))
            // Keep the empty slot: get() after remove() allocates a new ThreadLocal entry.
            // A null value releases the operation just as remove() does.
            operation.set(null)
            propagatedOperationId.set(context.operationId)
        }
        RuntimeHookGuard.run(onContextChanged)
        try {
            return block()
        } finally {
            RuntimeHookGuard.run { screenOverride.set(previousScreenOverride) }
            RuntimeHookGuard.run { owner.set(previousOwner) }
            RuntimeHookGuard.run { operation.set(previousOperation) }
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

    private fun JankHunterContext.matches(screen: String?, owner: String?, operationId: Long): Boolean {
        return this.screen == screen && this.owner == owner && this.operationId == operationId
    }

    private class CapturedContextCell(var value: JankHunterContext)
}

/** Reuses one mutable cell per participating thread instead of boxing every propagated ID. */
internal class PrimitiveLongThreadLocal {
    private val local = ThreadLocal<Cell>()

    fun get(): Long = local.get()?.value ?: 0L

    fun set(value: Long) {
        val cell = local.get()
        if (cell == null) {
            if (value > 0L) local.set(Cell(value))
        } else {
            // Retain one primitive cell per participating thread, including between scopes.
            cell.value = value.coerceAtLeast(0L)
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
