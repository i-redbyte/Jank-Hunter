package io.jankhunter.gradle

/** Includes select observed classes; their required ancestor helpers still respect explicit exclusions. */
internal class LifecycleAncestorAccessPolicy(private val excludePackages: Set<String>) {
    fun allows(header: LifecycleClassHeader): Boolean = header.programClass &&
        "Lio/jankhunter/annotations/JankHunterIgnore;" !in header.annotations &&
        InstrumentationMatcher.matchesNormalizedClassName(
            InstrumentationPackages.normalizePackage(header.name), emptySet(), excludePackages,
            includeWholeApplication = true,
        )
}
