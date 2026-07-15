package io.jankhunter.gradle

internal class InstrumentationMatcher(
    includePackages: Iterable<String>,
    excludePackages: Iterable<String>,
) {
    private val includes = InstrumentationPackages.normalizedPackages(includePackages).map(::PackageBoundary)
    private val excludes = InstrumentationPackages.normalizedPackages(
        excludePackages + InstrumentationPackages.builtinExcludePrefixes,
    ).map(::PackageBoundary)

    fun matches(className: String): Boolean {
        val normalized = InstrumentationPackages.normalizePackage(className)
        if (excludes.any { it.matches(normalized) }) return false
        if (InstrumentationPackages.isGeneratedAndroidClass(normalized)) return false
        return includes.any { it.matches(normalized) }
    }

    private class PackageBoundary(private val packageName: String) {
        private val childPrefix = "$packageName."

        fun matches(className: String): Boolean {
            return className == packageName || className.startsWith(childPrefix)
        }
    }
}
