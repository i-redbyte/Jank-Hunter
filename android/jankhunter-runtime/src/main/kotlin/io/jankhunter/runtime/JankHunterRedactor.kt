package io.jankhunter.runtime

fun interface JankHunterRedactor {
    fun redact(route: String?): String?

    companion object {
        @JvmStatic
        fun none(): JankHunterRedactor = JankHunterRedactor { it }

        /** Omits inputs above 4096 characters before applying the bounded redaction patterns. */
        @JvmStatic
        fun default(): JankHunterRedactor = DefaultRouteRedactor
    }
}

private object DefaultRouteRedactor : JankHunterRedactor {
    private const val MAX_ROUTE_INPUT_CHARS = 4096
    private val uuid = Regex("(?i)\\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\\b")
    private val email = Regex("[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\\.[A-Za-z]{2,}")
    private val longHex = Regex("(?i)\\b[0-9a-f]{16,}\\b")
    private val numericSegment = Regex("(?<=/)\\d+(?=/|$)")

    override fun redact(route: String?): String? {
        if (route == null || route.length > MAX_ROUTE_INPUT_CHARS) return null
        val withoutEmail = if ('@' in route) route.replace(email, "{email}") else route
        return withoutEmail
            .replace(uuid, "{uuid}")
            .replace(longHex, "{hex}")
            .replace(numericSegment, "{id}")
    }
}
