package io.jankhunter.gradle

class InstrumentationMatcher(
    includePackages: Iterable<String>,
    excludePackages: Iterable<String>,
) {
    private val includes = includePackages.map(::normalize).filter { it.isNotEmpty() }
    private val excludes = (excludePackages.map(::normalize) + builtinExcludes).filter { it.isNotEmpty() }

    fun matches(className: String): Boolean {
        val normalized = normalize(className)
        if (excludes.any { normalized.startsWith(it) }) return false
        if (includes.isEmpty()) return true
        return includes.any { normalized.startsWith(it) }
    }

    private fun normalize(value: String): String {
        return value.replace('/', '.').trim().removeSuffix(".")
    }

    private companion object {
        val builtinExcludes = listOf(
            "android.",
            "androidx.",
            "java.",
            "javax.",
            "kotlin.",
            "kotlinx.",
            "okhttp3.",
            "okio.",
            "io.jankhunter.",
        )
    }
}
