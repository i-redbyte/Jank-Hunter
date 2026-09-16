package io.jankhunter.runtime

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
    private const val FNV_OFFSET_BASIS = 0xcbf29ce484222325UL
    private const val FNV_PRIME = 0x100000001b3UL

    const val COMPOSE_COMPOSITION = 1
    const val COMPOSE_MEASURE = 2
    const val COMPOSE_LAYOUT = 3
    const val COMPOSE_DRAW = 4
    const val ROOM_DAO = 5
    const val WORKER = 6

    private const val THREAD_VARIANTS = 2
    private val callerTable = arrayOfNulls<SemanticCaller>((WORKER + 1) * THREAD_VARIANTS * OUTCOME_VARIANTS)

    init {
        for (kind in COMPOSE_COMPOSITION..WORKER) {
            repeat(THREAD_VARIANTS) { threadIndex ->
                val mainThread = threadIndex != 0
                if (kind == WORKER) {
                    JankHunterWorkerOutcome.entries.forEach { outcome ->
                        registerCaller(kind, mainThread, outcome)
                    }
                } else {
                    registerCaller(kind, mainThread, null)
                }
            }
        }
    }

    fun caller(kind: Int, mainThread: Boolean, outcome: JankHunterWorkerOutcome?): SemanticCaller? {
        if (kind !in COMPOSE_COMPOSITION..WORKER) return null
        val outcomeIndex = if (kind == WORKER) {
            (outcome ?: JankHunterWorkerOutcome.UNKNOWN).ordinal
        } else {
            NON_WORKER_OUTCOME_INDEX
        }
        return callerTable[callerIndex(kind, mainThread, outcomeIndex)]
    }

    fun callerLabel(kind: Int, mainThread: Boolean, outcome: JankHunterWorkerOutcome?): String? {
        return caller(kind, mainThread, outcome)?.name
    }

    private fun buildCallerLabel(kind: Int, mainThread: Boolean, outcome: JankHunterWorkerOutcome?): String? {
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
        return updateHash(FNV_OFFSET_BASIS, value).toLong()
    }

    fun stableId(prefix: String, value: CharSequence): Long {
        return updateHash(updateHash(FNV_OFFSET_BASIS, prefix), value).toLong()
    }

    private fun registerCaller(kind: Int, mainThread: Boolean, outcome: JankHunterWorkerOutcome?) {
        val name = checkNotNull(buildCallerLabel(kind, mainThread, outcome))
        val outcomeIndex = if (kind == WORKER) {
            checkNotNull(outcome).ordinal
        } else {
            NON_WORKER_OUTCOME_INDEX
        }
        callerTable[callerIndex(kind, mainThread, outcomeIndex)] = SemanticCaller(stableId(name), name)
    }

    private fun callerIndex(kind: Int, mainThread: Boolean, outcomeIndex: Int): Int {
        val threadIndex = if (mainThread) 1 else 0
        return (kind * THREAD_VARIANTS + threadIndex) * OUTCOME_VARIANTS + outcomeIndex
    }

    private fun updateHash(initial: ULong, value: CharSequence): ULong {
        var hash = initial
        var index = 0
        while (index < value.length) {
            val first = value[index]
            val codePoint = if (
                Character.isHighSurrogate(first) &&
                index + 1 < value.length &&
                Character.isLowSurrogate(value[index + 1])
            ) {
                index++
                Character.toCodePoint(first, value[index])
            } else if (Character.isSurrogate(first)) {
                REPLACEMENT_BYTE
            } else {
                first.code
            }
            hash = when {
                codePoint <= 0x7f -> hashByte(hash, codePoint)
                codePoint <= 0x7ff -> {
                    hashByte(hashByte(hash, 0xc0 or (codePoint ushr 6)), 0x80 or (codePoint and 0x3f))
                }
                codePoint <= 0xffff -> {
                    hashByte(
                        hashByte(hashByte(hash, 0xe0 or (codePoint ushr 12)), 0x80 or ((codePoint ushr 6) and 0x3f)),
                        0x80 or (codePoint and 0x3f),
                    )
                }
                else -> {
                    hashByte(
                        hashByte(
                            hashByte(hashByte(hash, 0xf0 or (codePoint ushr 18)), 0x80 or ((codePoint ushr 12) and 0x3f)),
                            0x80 or ((codePoint ushr 6) and 0x3f),
                        ),
                        0x80 or (codePoint and 0x3f),
                    )
                }
            }
            index++
        }
        return hash
    }

    private fun hashByte(hash: ULong, value: Int): ULong = (hash xor value.toULong()) * FNV_PRIME

    private const val OUTCOME_VARIANTS = 5
    private const val NON_WORKER_OUTCOME_INDEX = 0
    private const val REPLACEMENT_BYTE = 0x3f
}

internal class SemanticCaller(
    val id: Long,
    val name: String,
)
