package io.jankhunter.runtime

fun interface JankHunterRedactor {
    fun redact(route: String?): String?

    companion object {
        @JvmStatic
        fun none(): JankHunterRedactor = JankHunterRedactor { it }

        @JvmStatic
        fun default(): JankHunterRedactor = DefaultRouteRedactor
    }
}

private object DefaultRouteRedactor : JankHunterRedactor {
    private val uuid = Regex("(?i)\\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\\b")
    private val email = Regex("[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\\.[A-Za-z]{2,}")
    private val longHex = Regex("(?i)\\b[0-9a-f]{16,}\\b")
    private val numericSegment = Regex("(?<=/)\\d+(?=/|$)")

    override fun redact(route: String?): String? {
        if (route == null) return null
        return route
            .replace(email, "{email}")
            .replace(uuid, "{uuid}")
            .replace(longHex, "{hex}")
            .replace(numericSegment, "{id}")
    }
}
