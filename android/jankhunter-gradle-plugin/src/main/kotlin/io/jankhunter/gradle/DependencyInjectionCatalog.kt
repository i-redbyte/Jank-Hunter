package io.jankhunter.gradle

import com.android.build.api.instrumentation.ClassData

internal enum class DependencyInjectionFramework(val wireName: String) {
    DAGGER2("dagger2"),
    HILT("hilt"),
    KOIN("koin"),
}

internal data class DependencyInjectionClassRecord(
    val name: String,
    val framework: DependencyInjectionFramework,
    val roles: Set<String>,
    val generated: Boolean,
    val scopes: Set<String> = emptySet(),
    val components: Set<String> = emptySet(),
)

internal data class DependencyInjectionEdgeRecord(
    val consumer: String,
    val dependency: String,
    val framework: DependencyInjectionFramework,
    val injectionKind: String,
    val site: String,
    val qualifiers: Set<String> = emptySet(),
    val resolution: String,
)

internal object DependencyInjectionClassMatcher {
    fun shouldScan(classData: ClassData, includePackages: Iterable<String>): Boolean {
        if (InstrumentationPackages.isGeneratedAndroidClass(classData.className)) return false
        if (classData.className.normalizedClassName().startsWith("io.jankhunter.")) return false
        if (isGeneratedDiClass(classData)) return true
        val className = classData.className.normalizedClassName()
        return includePackages
            .map { it.normalizedClassName() }
            .filter(String::isNotEmpty)
            .any { include -> className == include || className.startsWith("$include.") }
    }

    fun isGeneratedDiClass(classData: ClassData): Boolean {
        return isGeneratedDiClass(
            className = classData.className,
            hierarchy = classData.superClasses + classData.interfaces,
        )
    }

    fun isGeneratedDiClass(className: String, hierarchy: Iterable<String> = emptyList()): Boolean {
        val normalized = className.normalizedClassName()
        val simpleName = normalized.substringAfterLast('.')
        if (normalized.startsWith("hilt_aggregated_deps.")) return true
        if (normalized.startsWith("dagger.hilt.internal.aggregatedroot.codegen.")) return true
        if (normalized.startsWith("dagger.hilt.internal.componenttreedeps.")) return true
        if (normalized.startsWith("org.koin.ksp.generated.")) return true
        if (simpleName.startsWith("Dagger") || simpleName.startsWith("Hilt_")) return true
        if (simpleName.endsWith("_Factory") || simpleName.endsWith("_MembersInjector")) return true
        if (simpleName.contains("_HiltComponents") || simpleName.endsWith("ModuleGen")) return true
        if (simpleName.endsWith("_GeneratedInjector") || simpleName.endsWith("_HiltModules")) return true
        if (simpleName.endsWith("_ComponentTreeDeps") || simpleName.endsWith("_AggregatedDeps")) return true
        return hierarchy.any { parent ->
            when (parent.normalizedClassName()) {
                "dagger.internal.Factory",
                "dagger.MembersInjector",
                "dagger.internal.Provider" -> true
                else -> false
            }
        }
    }

    fun generatedFramework(classData: ClassData): DependencyInjectionFramework? {
        val normalized = classData.className.normalizedClassName()
        val hierarchy = (classData.superClasses + classData.interfaces).map { it.normalizedClassName() }
        return when {
            normalized.startsWith("org.koin.ksp.generated.") || normalized.substringAfterLast('.').endsWith("ModuleGen") -> {
                DependencyInjectionFramework.KOIN
            }
            normalized.startsWith("hilt_aggregated_deps.") ||
                normalized.startsWith("dagger.hilt.") ||
                normalized.substringAfterLast('.').startsWith("Hilt_") ||
                normalized.substringAfterLast('.').contains("_HiltComponents") ||
                normalized.substringAfterLast('.').endsWith("_GeneratedInjector") ||
                normalized.substringAfterLast('.').endsWith("_HiltModules") ||
                normalized.substringAfterLast('.').endsWith("_ComponentTreeDeps") ||
                normalized.substringAfterLast('.').endsWith("_AggregatedDeps") -> {
                DependencyInjectionFramework.HILT
            }
            hierarchy.any { it == "dagger.internal.Factory" || it == "dagger.MembersInjector" } ||
                normalized.substringAfterLast('.').startsWith("Dagger") ||
                normalized.substringAfterLast('.').endsWith("_Factory") ||
                normalized.substringAfterLast('.').endsWith("_MembersInjector") -> {
                DependencyInjectionFramework.DAGGER2
            }
            else -> null
        }
    }

    private fun String.normalizedClassName(): String {
        return replace('/', '.').trim().removePrefix("L").removeSuffix(";")
    }
}

internal object DependencyInjectionCatalogWriter {
    fun write(
        directoryPath: String,
        className: String,
        classRecord: DependencyInjectionClassRecord?,
        edges: Collection<DependencyInjectionEdgeRecord>,
    ) {
        if (directoryPath.isBlank()) return
        val records = buildList {
            classRecord?.let { add(classRecord(it)) }
            edges
                .asSequence()
                .filter { it.consumer.isNotBlank() && it.dependency.isNotBlank() && it.consumer != it.dependency }
                .distinct()
                .sortedWith(
                    compareBy<DependencyInjectionEdgeRecord> { it.consumer }
                        .thenBy { it.dependency }
                        .thenBy { it.injectionKind }
                        .thenBy { it.site }
                        .thenBy { it.framework.wireName },
                )
                .mapTo(this, ::edgeRecord)
        }
        InstrumentationArtifactFiles.writeClassShard(
            directoryPath,
            className,
            records.joinToString(separator = "\n", postfix = if (records.isEmpty()) "" else "\n"),
        )
    }

    fun metadataRecord(variantName: String): String {
        return buildString {
            append("{\"format\":")
            append(ArtifactSchemas.DEPENDENCY_INJECTION_CATALOG_FORMAT)
            append(",\"kind\":\"metadata\",\"variant\":\"")
            append(escape(variantName))
            append("\",\"semantics\":\"build_time_di\"")
            append(",\"edgeDirection\":\"consumer_to_dependency\"")
            append(",\"runtimeTracing\":false,\"affectsScore\":false}")
        }
    }

    private fun classRecord(record: DependencyInjectionClassRecord): String {
        return buildString {
            append("{\"format\":")
            append(ArtifactSchemas.DEPENDENCY_INJECTION_CATALOG_FORMAT)
            append(",\"kind\":\"class\",\"name\":\"")
            append(escape(record.name))
            append("\",\"framework\":\"")
            append(record.framework.wireName)
            append("\",\"roles\":")
            append(array(record.roles))
            append(",\"generated\":")
            append(record.generated)
            append(",\"scopes\":")
            append(array(record.scopes))
            append(",\"components\":")
            append(array(record.components))
            append('}')
        }
    }

    private fun edgeRecord(record: DependencyInjectionEdgeRecord): String {
        return buildString {
            append("{\"format\":")
            append(ArtifactSchemas.DEPENDENCY_INJECTION_CATALOG_FORMAT)
            append(",\"kind\":\"edge\",\"consumer\":\"")
            append(escape(record.consumer))
            append("\",\"dependency\":\"")
            append(escape(record.dependency))
            append("\",\"framework\":\"")
            append(record.framework.wireName)
            append("\",\"injectionKind\":\"")
            append(escape(record.injectionKind))
            append("\",\"site\":\"")
            append(escape(record.site))
            append("\",\"qualifiers\":")
            append(array(record.qualifiers))
            append(",\"resolution\":\"")
            append(escape(record.resolution))
            append("\"}")
        }
    }

    private fun array(values: Iterable<String>): String {
        return values
            .asSequence()
            .map(String::trim)
            .filter(String::isNotEmpty)
            .distinct()
            .sorted()
            .joinToString(prefix = "[", postfix = "]") { "\"${escape(it)}\"" }
    }

    private fun escape(value: String): String {
        return value
            .replace("\\", "\\\\")
            .replace("\"", "\\\"")
            .replace("\n", "\\n")
            .replace("\r", "\\r")
            .replace("\t", "\\t")
    }
}
