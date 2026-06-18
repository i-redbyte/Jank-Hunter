package io.jankhunter.gradle

class InstrumentationMatcher(
    includePackages: Iterable<String>,
    excludePackages: Iterable<String>,
    private val allowEmptyIncludes: Boolean = false,
) {
    private val includes = includePackages.map(InstrumentationPackages::normalizePackage).filter { it.isNotEmpty() }
    private val excludes = (
        excludePackages.map(InstrumentationPackages::normalizePackage) +
            InstrumentationPackages.builtinExcludePrefixes
        ).filter { it.isNotEmpty() }

    fun matches(className: String): Boolean {
        val normalized = InstrumentationPackages.normalizePackage(className)
        if (excludes.any { normalized.startsWith(it) }) return false
        if (InstrumentationPackages.isGeneratedAndroidClass(normalized)) return false
        if (includes.isEmpty() && !allowEmptyIncludes) return false
        if (includes.isEmpty()) return true
        return includes.any { normalized.startsWith(it) }
    }
}
