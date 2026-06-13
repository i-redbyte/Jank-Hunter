package io.jankhunter.runtime

fun interface JankHunterProcessNameRedactor {
    fun redact(processName: String?): String?

    companion object {
        @JvmStatic
        fun none(): JankHunterProcessNameRedactor = JankHunterProcessNameRedactor { it }
    }
}
