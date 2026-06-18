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

    private fun addPackages(target: MutableSet<String>, values: Iterable<String>) {
        values.forEach { addPackage(target, it) }
    }

    private fun addPackage(target: MutableSet<String>, value: String?) {
        val normalized = value?.let(::normalizePackage) ?: return
        if (normalized.isNotEmpty()) {
            target.add(normalized)
        }
    }
}
