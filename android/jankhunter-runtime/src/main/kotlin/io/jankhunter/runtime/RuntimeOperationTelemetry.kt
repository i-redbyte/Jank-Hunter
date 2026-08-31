package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.Jhlog
import java.util.concurrent.atomic.AtomicLong

internal class RuntimeOperationTelemetry(
    private val contexts: ContextTracker,
    private val sinkProvider: () -> OperationEventSink?,
    private val elapsedRealtimeUs: RuntimeLongSource,
    private val onContextChanged: () -> Unit,
    initialOperationId: Long = 1L,
) : JankHunterOperationController {
    private val nextOperationId = AtomicLong(initialOperationId)

    init {
        require(initialOperationId > 0L) { "Initial operation ID must be positive" }
    }

    fun start(
        name: String,
        kind: JankHunterOperationKind,
        budgetMs: Long,
        attributes: JankHunterOperationAttributes,
    ): JankHunterOperation {
        val operationName = name.trim()
        require(operationName.isNotEmpty() && operationName != "unknown") { "Operation name must be known" }
        require(budgetMs >= 0L) { "Operation budget must be non-negative" }
        val sink = sinkProvider() ?: return JankHunterOperation.NONE
        val operationId = nextOperationId.getAndIncrement()
        if (operationId <= 0L) return JankHunterOperation.NONE
        val previous = contexts.currentOperationOrNull()
        val budgetUs = millisecondsToMicroseconds(budgetMs)
        val screen = contexts.currentScreenOrNull()
        val owner = contexts.ownerOrNull()
        val operation = JankHunterOperation(
            id = operationId,
            parentId = contexts.currentOperationId(),
            name = operationName,
            kind = kind,
            startedAtUs = elapsedRealtimeUs.getAsLong().coerceAtLeast(0L),
            budgetUs = budgetUs,
            screen = screen,
            owner = owner,
            attributes = attributes,
            previousOperation = previous,
            sink = sink,
            controller = this,
        )
        contexts.activateOperation(operation)
        notifyContextChanged()
        val accepted = guardedWrite {
            sink.operation(
                name = operationName,
                operationId = operationId,
                parentId = operation.parentId,
                phase = Jhlog.OPERATION_PHASE_STARTED,
                kind = kind.wireValue,
                outcome = 0L,
                durationUs = 0L,
                budgetUs = budgetUs,
                screen = screen,
                owner = owner,
                attributes = attributes,
            )
        }
        if (accepted) return operation

        operation.abandon()
        contexts.deactivateOperation(operation)
        operation.releaseCompletionReferences()
        notifyContextChanged()
        return JankHunterOperation.NONE
    }

    override fun complete(operation: JankHunterOperation, outcome: JankHunterOperationOutcome) {
        val finishedAtUs = elapsedRealtimeUs.getAsLong().coerceAtLeast(operation.startedAtUs)
        try {
            guardedWrite {
                operation.sink?.operation(
                    name = operation.name,
                    operationId = operation.id,
                    parentId = operation.parentId,
                    phase = Jhlog.OPERATION_PHASE_FINISHED,
                    kind = operation.kind.wireValue,
                    outcome = outcome.wireValue,
                    durationUs = finishedAtUs - operation.startedAtUs,
                    budgetUs = operation.budgetUs,
                    screen = operation.screen,
                    owner = operation.owner,
                    attributes = operation.attributes,
                ) == true
            }
        } finally {
            contexts.deactivateOperation(operation)
            notifyContextChanged()
        }
    }

    private inline fun guardedWrite(write: () -> Boolean): Boolean {
        return try {
            write()
        } catch (throwable: Throwable) {
            RuntimeHookGuard.rethrowFatal(throwable)
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.UNCLASSIFIED)
            false
        }
    }

    private fun notifyContextChanged() {
        RuntimeHookGuard.run(onContextChanged)
    }

    private fun millisecondsToMicroseconds(value: Long): Long {
        return if (value > Long.MAX_VALUE / MICROSECONDS_PER_MILLISECOND) {
            Long.MAX_VALUE
        } else {
            value * MICROSECONDS_PER_MILLISECOND
        }
    }

    private companion object {
        const val MICROSECONDS_PER_MILLISECOND = 1_000L
    }
}
