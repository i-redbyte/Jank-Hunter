package io.jankhunter.runtime.internal.system

/** Kernel observations only. Java IDs/states cannot be inferred from names or Linux IDs. */
internal data class ThreadObservation(
    val tid: Long,
    val name: String,
    val osState: Char,
    val startTicks: Long,
    val userTicks: Long,
    val systemTicks: Long,
    val priority: Long,
    val nice: Long,
    val java: JavaThreadObservation? = null,
) {
    fun sameGeneration(other: ThreadObservation): Boolean = tid == other.tid && startTicks == other.startTicks
}

private val THREAD_STAT_WHITESPACE = Regex("\\s+")

/** Pure parser for Linux stat fields 1..22; comm may contain spaces and closing parentheses. */
internal fun parseThreadStat(stat: String): ThreadObservation? {
    val open = stat.indexOf(" (")
    val close = stat.lastIndexOf(") ")
    if (open < 1 || close <= open + 1) return null
    val tid = stat.substring(0, open).toLongOrNull()?.takeIf { it > 0 } ?: return null
    val fields = stat.substring(close + 2).trim().split(THREAD_STAT_WHITESPACE, limit = 21)
    if (fields.size < 20 || fields[0].length != 1) return null
    val user = fields[11].toLongOrNull()?.takeIf { it >= 0 } ?: return null
    val system = fields[12].toLongOrNull()?.takeIf { it >= 0 } ?: return null
    val start = fields[19].toLongOrNull()?.takeIf { it >= 0 } ?: return null
    return ThreadObservation(
        tid, stat.substring(open + 2, close).take(256), fields[0][0], start, user, system,
        fields[15].toLongOrNull() ?: return null,
        fields[16].toLongOrNull() ?: return null,
    )
}
