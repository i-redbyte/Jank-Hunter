package io.jankhunter.gradle

internal object InstrumentationPackages {
    fun effectiveIncludes(
        manualIncludes: Iterable<String>,
        includeWholeApplication: Boolean,
        androidNamespace: String?,
    ): List<String> {
        val packages = linkedSetOf<String>()
        addPackages(packages, manualIncludes)
        if (includeWholeApplication) {
            addPackage(packages, androidNamespace)
        }
        return packages.toList()
    }

    private fun addPackages(target: MutableSet<String>, values: Iterable<String>) {
        values.forEach { addPackage(target, it) }
    }

    private fun addPackage(target: MutableSet<String>, value: String?) {
        val normalized = value?.trim()?.removeSuffix(".") ?: return
        if (normalized.isNotEmpty()) {
            target.add(normalized)
        }
    }
}
