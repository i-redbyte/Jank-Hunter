package io.jankhunter.gradle

internal object InstrumentationPackages {
    val builtinExcludePrefixes: List<String> = listOf(
        "android.",
        "androidx.",
        "java.",
        "javax.",
        "kotlin.",
        "kotlinx.",
        "okhttp3.",
        "okio.",
        "org.jetbrains.",
        "io.jankhunter.",
    )

    fun effectiveIncludes(
        manualIncludes: Iterable<String>,
        androidNamespace: String?,
    ): Set<String> {
        val packages = linkedSetOf<String>()
        // Namespace is the safe, deterministic project boundary. Manual includes may extend
        // it inside InstrumentationScope.PROJECT, but an empty DSL must never mean "everything".
        addPackage(packages, androidNamespace)
        packages.addAll(normalizedPackages(manualIncludes))
        return packages
    }

    fun normalizedPackages(values: Iterable<String>): Set<String> {
        return values.mapNotNullTo(linkedSetOf()) { value ->
            normalizePackage(value).takeIf(String::isNotEmpty)
        }
    }

    fun normalizePackage(value: String): String {
        return value.replace('/', '.').trim().removeSuffix(".")
    }

    fun isBuiltinExcluded(value: String): Boolean {
        val normalized = normalizePackage(value)
        return builtinExcludePrefixes.any { normalized.startsWith(it) }
    }

    fun isGeneratedAndroidClass(value: String): Boolean {
        val simpleName = normalizePackage(value).substringAfterLast('.')
        return simpleName == "R" ||
            simpleName.startsWith("R$") ||
            simpleName == "BuildConfig" ||
            simpleName == "Manifest" ||
            simpleName.startsWith("Manifest$") ||
            simpleName == "BR"
    }

    private fun addPackage(target: MutableSet<String>, value: String?) {
        val normalized = value?.let(::normalizePackage) ?: return
        if (normalized.isNotEmpty()) {
            target.add(normalized)
        }
    }
}
