package io.jankhunter.runtime

/**
 * Reusable definition of an application-owned scenario.
 *
 * Create and cache definitions for low-cardinality scenarios. Starting a scenario emits the
 * existing USER operation; [startStage] emits the existing STAGE operation and relies on the
 * current operation context for its parent. No new wire event or process-wide registry is used.
 */
class JankHunterScenario private constructor(
    val id: String,
    val revision: String,
    @PublishedApi internal val operationAttributes: JankHunterOperationAttributes,
) {
    @JvmOverloads
    fun start(
        name: String = id,
        budgetMs: Long = 0L,
    ): JankHunterOperation = JankHunterTelemetry.startOperation(
        name = name,
        kind = JankHunterOperationKind.USER,
        budgetMs = budgetMs,
        attributes = operationAttributes,
    )

    @JvmOverloads
    fun startStage(
        name: String,
        budgetMs: Long = 0L,
    ): JankHunterOperation = JankHunterTelemetry.startOperation(
        name = name,
        kind = JankHunterOperationKind.STAGE,
        budgetMs = budgetMs,
    )

    @JvmSynthetic
    inline fun <T> trace(
        name: String = id,
        budgetMs: Long = 0L,
        block: () -> T,
    ): T = JankHunterTelemetry.traceOperation(
        name = name,
        kind = JankHunterOperationKind.USER,
        budgetMs = budgetMs,
        attributes = operationAttributes,
        block = block,
    )

    @JvmSynthetic
    inline fun <T> traceStage(
        name: String,
        budgetMs: Long = 0L,
        block: () -> T,
    ): T = JankHunterTelemetry.traceOperation(
        name = name,
        kind = JankHunterOperationKind.STAGE,
        budgetMs = budgetMs,
        block = block,
    )

    companion object {
        const val SCENARIO_ATTRIBUTE = "jh.scenario"
        const val REVISION_ATTRIBUTE = "jh.scenario_rev"
        const val MAX_VALUE_UTF8_BYTES = 128
        const val MAX_CONDITIONS = JankHunterOperationAttributes.MAX_SIZE - 2

        @JvmStatic
        @JvmOverloads
        fun create(
            id: String,
            revision: String,
            conditions: JankHunterOperationAttributes = JankHunterOperationAttributes.EMPTY,
        ): JankHunterScenario {
            requireScenarioValue("Scenario ID", id)
            requireScenarioValue("Scenario revision", revision)
            require(conditions.size <= MAX_CONDITIONS) {
                "Scenario conditions exceed $MAX_CONDITIONS pairs"
            }
            var index = 0
            while (index < conditions.size) {
                val key = conditions.key(index)
                require(key != SCENARIO_ATTRIBUTE && key != REVISION_ATTRIBUTE) {
                    "Scenario condition '$key' collides with a reserved attribute"
                }
                requireScenarioValue("Scenario condition '$key'", conditions.value(index))
                index++
            }
            return JankHunterScenario(
                id = id,
                revision = revision,
                operationAttributes = conditions.prepended(
                    SCENARIO_ATTRIBUTE,
                    id,
                    REVISION_ATTRIBUTE,
                    revision,
                ),
            )
        }

        private fun requireScenarioValue(label: String, value: String) {
            require(value.isNotBlank() && value != "unknown") { "$label must be known" }
            require(utf8LengthAtMost(value, MAX_VALUE_UTF8_BYTES)) {
                "$label exceeds $MAX_VALUE_UTF8_BYTES UTF-8 bytes"
            }
        }

        private fun utf8LengthAtMost(value: String, limit: Int): Boolean {
            var bytes = 0
            var index = 0
            while (index < value.length) {
                val current = value[index]
                bytes += when {
                    current.code <= 0x7f -> 1
                    current.code <= 0x7ff -> 2
                    current.isHighSurrogate() -> {
                        if (index + 1 >= value.length || !value[index + 1].isLowSurrogate()) return false
                        index++
                        4
                    }
                    current.isLowSurrogate() -> return false
                    else -> 3
                }
                if (bytes > limit) return false
                index++
            }
            return true
        }
    }
}
