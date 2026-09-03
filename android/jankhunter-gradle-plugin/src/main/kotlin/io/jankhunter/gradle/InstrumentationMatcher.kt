package io.jankhunter.gradle

internal class InstrumentationMatcher(
    includePackages: Iterable<String>,
    excludePackages: Iterable<String>,
    private val includeWholeApplication: Boolean = false,
) {
    private val includes = InstrumentationPackages.normalizedPackages(includePackages).map(::PackageBoundary)
    private val excludes = InstrumentationPackages.normalizedPackages(
        excludePackages + InstrumentationPackages.builtinExcludePrefixes,
    ).map(::PackageBoundary)

    fun matches(className: String): Boolean {
        val normalized = InstrumentationPackages.normalizePackage(className)
        if (excludes.any { it.matches(normalized) }) return false
        if (InstrumentationPackages.isGeneratedAndroidClass(normalized)) return false
        return includeWholeApplication || includes.any { it.matches(normalized) }
    }

    private class PackageBoundary(private val packageName: String) {
        private val childPrefix = "$packageName."

        fun matches(className: String): Boolean {
            return className == packageName || className.startsWith(childPrefix)
        }
    }

    companion object {
        fun matchesNormalizedClassName(
            normalizedClassName: String,
            includePackages: Iterable<String>,
            excludePackages: Iterable<String>,
            includeWholeApplication: Boolean = false,
            includeBuiltinExcludes: Boolean = true,
        ): Boolean {
            if (matchesAnyBoundary(normalizedClassName, excludePackages)) return false
            if (includeBuiltinExcludes &&
                matchesAnyBoundary(normalizedClassName, InstrumentationPackages.builtinExcludePrefixes)
            ) {
                return false
            }
            if (InstrumentationPackages.isGeneratedAndroidClass(normalizedClassName)) return false
            return includeWholeApplication || matchesAnyBoundary(normalizedClassName, includePackages)
        }

        private fun matchesAnyBoundary(className: String, packages: Iterable<String>): Boolean {
            return packages.any { packageName ->
                className == packageName ||
                    className.length > packageName.length && className.startsWith(packageName) &&
                    className[packageName.length] == '.'
            }
        }
    }
}
