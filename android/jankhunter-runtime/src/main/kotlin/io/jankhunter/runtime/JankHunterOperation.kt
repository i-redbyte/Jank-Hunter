package io.jankhunter.runtime

import java.util.concurrent.atomic.AtomicIntegerFieldUpdater

/**
 * A process-local handle for one measured operation.
 *
 * Completion is lock-free and exactly-once, including when competing threads race to finish the
 * same operation. [close] records a successful outcome.
 */
class JankHunterOperation internal constructor(
    val id: Long,
    val parentId: Long,
    val name: String,
    val kind: JankHunterOperationKind,
    internal val startedAtUs: Long,
    internal val budgetUs: Long,
    internal var screen: String?,
    internal var owner: String?,
    internal var attributes: JankHunterOperationAttributes,
    @Volatile internal var previousOperation: JankHunterOperation?,
    internal var sink: OperationEventSink?,
    private var controller: JankHunterOperationController?,
    initialState: Int = STATE_ACTIVE,
) : AutoCloseable {
    @JvmSynthetic
    @JvmField
    @Volatile
    internal var completionState: Int = initialState

    val isFinished: Boolean
        get() = completionState != STATE_ACTIVE

    fun finish(outcome: JankHunterOperationOutcome): Boolean {
        if (!STATE.compareAndSet(this, STATE_ACTIVE, STATE_FINISHED)) return false
        try {
            controller?.complete(this, outcome)
        } finally {
            releaseCompletionReferences()
        }
        return true
    }

    fun success(): Boolean = finish(JankHunterOperationOutcome.SUCCESS)

    fun failure(): Boolean = finish(JankHunterOperationOutcome.FAILURE)

    fun cancel(): Boolean = finish(JankHunterOperationOutcome.CANCELLED)

    fun timeout(): Boolean = finish(JankHunterOperationOutcome.TIMEOUT)

    override fun close() {
        success()
    }

    internal fun abandon(): Boolean = STATE.compareAndSet(this, STATE_ACTIVE, STATE_FINISHED)

    internal fun releaseCompletionReferences() {
        screen = null
        owner = null
        attributes = JankHunterOperationAttributes.EMPTY
        sink = null
        controller = null
    }

    internal companion object {
        private const val STATE_ACTIVE = 0
        private const val STATE_FINISHED = 1

        private val STATE = AtomicIntegerFieldUpdater.newUpdater(
            JankHunterOperation::class.java,
            "completionState",
        )

        val NONE = JankHunterOperation(
            id = 0L,
            parentId = 0L,
            name = "",
            kind = JankHunterOperationKind.SYSTEM,
            startedAtUs = 0L,
            budgetUs = 0L,
            screen = null,
            owner = null,
            attributes = JankHunterOperationAttributes.EMPTY,
            previousOperation = null,
            sink = null,
            controller = null,
            initialState = STATE_FINISHED,
        )
    }
}

internal interface JankHunterOperationController {
    fun complete(operation: JankHunterOperation, outcome: JankHunterOperationOutcome)
}

internal interface OperationEventSink {
    fun operation(
        name: String,
        operationId: Long,
        parentId: Long,
        phase: Long,
        kind: Long,
        outcome: Long,
        durationUs: Long,
        budgetUs: Long,
        screen: String?,
        owner: String?,
        attributes: JankHunterOperationAttributes,
    ): Boolean
}
