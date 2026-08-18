package io.jankhunter.runtime

import java.nio.charset.StandardCharsets

/** Compose phase used by explicit tracing around custom layout and drawing code. */
enum class JankHunterComposePhase(internal val wireName: String) {
    COMPOSITION("composition"),
    MEASURE("measure"),
    LAYOUT("layout"),
    DRAW("draw"),
}

/** Outcome of an explicitly traced WorkManager worker execution. */
enum class JankHunterWorkerOutcome(
    internal val wireName: String,
    internal val code: Int,
) {
    SUCCESS("success", 0),
    FAILURE("failure", 1),
    RETRY("retry", 2),
    CANCELLED("cancelled", 3),
    UNKNOWN("unknown", 4),
}

internal object JankHunterSemanticWork {
    const val COMPOSE_COMPOSITION = 1
    const val COMPOSE_MEASURE = 2
    const val COMPOSE_LAYOUT = 3
    const val COMPOSE_DRAW = 4
    const val ROOM_DAO = 5
    const val WORKER = 6

    fun callerLabel(kind: Int, mainThread: Boolean, outcome: JankHunterWorkerOutcome?): String? {
        val domain = when (kind) {
            COMPOSE_COMPOSITION -> "compose.composition"
            COMPOSE_MEASURE -> "compose.measure"
            COMPOSE_LAYOUT -> "compose.layout"
            COMPOSE_DRAW -> "compose.draw"
            ROOM_DAO -> "room.dao"
            WORKER -> "worker.${outcome?.wireName ?: JankHunterWorkerOutcome.UNKNOWN.wireName}"
            else -> return null
        }
        val thread = if (mainThread) "main" else "background"
        return "jankhunter.semantic.v1.$domain.$thread"
    }

    fun stableId(value: String): Long {
        var hash = FNV_OFFSET_BASIS
        for (byte in value.toByteArray(StandardCharsets.UTF_8)) {
            hash = hash xor byte.toUByte().toULong()
            hash *= FNV_PRIME
        }
        return hash.toLong()
    }

    private val FNV_OFFSET_BASIS = 0xcbf29ce484222325UL
    private val FNV_PRIME = 0x100000001b3UL
}
