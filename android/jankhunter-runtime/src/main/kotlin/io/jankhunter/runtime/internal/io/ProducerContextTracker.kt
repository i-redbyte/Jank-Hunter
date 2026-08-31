package io.jankhunter.runtime.internal.io

/** Per-producer attribution containing only immutable values serialized into the log. */
internal class ProducerContextTracker {
    private val local = ThreadLocal<LogEventContext>()

    fun update(
        screen: String?,
        owner: String?,
        operationId: Long,
    ) {
        val current = capture() ?: LogEventContext.EMPTY
        if (current.matches(screen, owner, operationId)) return
        set(LogEventContext.of(screen, owner, operationId))
    }

    fun capture(): LogEventContext? = local.get()

    fun capture(screen: String?, owner: String?): LogEventContext? {
        val current = capture()
        val operationId = current?.operationId ?: 0L
        return when {
            current != null && current.matches(screen, owner, operationId) -> current
            LogEventContext.EMPTY.matches(screen, owner, operationId) -> null
            else -> LogEventContext.of(screen, owner, operationId)
        }
    }

    fun captureOperation(screen: String?, owner: String?, operationId: Long): LogEventContext {
        val current = capture()
        return if (current != null && current.matches(screen, owner, operationId)) {
            current
        } else {
            LogEventContext.of(screen, owner, operationId)
        }
    }

    private fun set(context: LogEventContext) {
        if (context == LogEventContext.EMPTY) {
            local.remove()
        } else {
            local.set(context)
        }
    }

}
