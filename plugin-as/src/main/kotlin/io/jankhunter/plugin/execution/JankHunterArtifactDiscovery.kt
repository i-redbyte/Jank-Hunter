package io.jankhunter.plugin.execution

import com.intellij.openapi.project.Project
import java.io.File
import java.nio.file.Files
import java.nio.file.Path
import kotlin.io.path.isExecutable
import kotlin.io.path.isRegularFile

data class JankHunterArtifactSet(
    val variant: String,
    val ownerMap: String = "",
    val mapping: String = "",
    val classGraph: String = "",
    val diagnostics: String = "",
    val diCatalog: String = "",
) {
    val isEmpty: Boolean
        get() = ownerMap.isBlank() && mapping.isBlank() && classGraph.isBlank() && diagnostics.isBlank() && diCatalog.isBlank()

    fun displayName(): String = if (variant.isBlank()) "detected" else variant
}

object JankHunterArtifactDiscovery {
    private val skippedDirectories = setOf(
        ".git",
        ".gradle",
        ".idea",
        ".intellijPlatform",
        "node_modules",
        "tmp",
    )

    fun detectCli(project: Project): String {
        val basePath = project.basePath?.let(Path::of)
        val candidates = buildList {
            addAll(
                listOfNotNull(
                    basePath?.resolve(".jankhunter/bin/jankhunter"),
                    basePath?.resolve("cli/bin/jankhunter"),
                    basePath?.resolve("../cli/bin/jankhunter")?.normalize(),
                    basePath?.resolve("../../cli/bin/jankhunter")?.normalize(),
                    System.getProperty("user.home")?.let { Path.of(it).resolve(".jankhunter/bin/jankhunter") },
                    Path.of("/opt/homebrew/bin/jankhunter"),
                    Path.of("/usr/local/bin/jankhunter"),
                ),
            )
            System.getenv("PATH")
                ?.split(File.pathSeparator)
                ?.mapNotNull { raw ->
                    raw.trim().takeIf(String::isNotEmpty)?.let { Path.of(it).resolve("jankhunter") }
                }
                ?.let(::addAll)
        }

        return candidates
            .firstOrNull { path -> path.isRegularFile() && path.isExecutable() }
            ?.toAbsolutePath()
            ?.normalize()
            ?.toString()
            ?: "jankhunter"
    }

    fun findArtifactSets(project: Project): List<JankHunterArtifactSet> {
        val root = project.basePath?.let { File(it) }?.takeIf { it.isDirectory } ?: return emptyList()
        return findArtifactSets(root)
    }

    fun findArtifactSets(root: File): List<JankHunterArtifactSet> {
        val grouped = linkedMapOf<String, MutableArtifactSet>()
        findBuildDirectories(root).forEach { buildDirectory ->
            val module = buildDirectory.parentFile
                ?.relativeToOrNull(root)
                ?.invariantSeparatorsPath
                .orEmpty()
                .ifBlank { ":" }
            val generatedRoot = File(buildDirectory, "generated/jankhunter")
            walkFiles(generatedRoot).forEach { file ->
                val variant = file.relativeToOrNull(generatedRoot)
                    ?.invariantSeparatorsPath
                    ?.substringBefore('/')
                    .orEmpty()
                val key = "$module|$variant"
                val artifacts = grouped.getOrPut(key) { MutableArtifactSet(displayVariant(module, variant)) }
                when (file.name) {
                    "owner-map.json" -> artifacts.ownerMap = file.path
                    "class-graph.jsonl" -> artifacts.classGraph = file.path
                    "instrumentation-diagnostics.jsonl" -> artifacts.diagnostics = file.path
                    "di-catalog.jsonl" -> artifacts.diCatalog = file.path
                }
            }

            val mappingRoot = File(buildDirectory, "outputs/mapping")
            walkFiles(mappingRoot)
                .filter { file -> file.name == "mapping.txt" }
                .forEach { file ->
                    val variant = file.relativeToOrNull(mappingRoot)
                        ?.invariantSeparatorsPath
                        ?.substringBefore('/')
                        .orEmpty()
                    val key = "$module|$variant"
                    grouped.getOrPut(key) { MutableArtifactSet(displayVariant(module, variant)) }.mapping = file.path
                }
        }

        return grouped
            .map { (_, mutable) ->
                JankHunterArtifactSet(
                    variant = mutable.variant,
                    ownerMap = mutable.ownerMap,
                    mapping = mutable.mapping,
                    classGraph = mutable.classGraph,
                    diagnostics = mutable.diagnostics,
                    diCatalog = mutable.diCatalog,
                )
            }
            .filterNot(JankHunterArtifactSet::isEmpty)
            .sortedWith(
                compareByDescending<JankHunterArtifactSet> { set -> score(set) }
                    .thenByDescending { set -> latestModified(set) }
                    .thenBy { set -> set.variant },
            )
    }

    fun findRecentLogs(project: Project, limit: Int = 20): List<String> {
        val root = project.basePath?.let { File(it) }?.takeIf { it.isDirectory } ?: return emptyList()
        return root.walkTopDown()
            .maxDepth(MAX_LOG_SEARCH_DEPTH)
            .onEnter { file -> file.name !in skippedDirectories }
            .filter { it.isFile && it.extension.equals("jhlog", ignoreCase = true) }
            .sortedByDescending { it.lastModified() }
            .take(limit)
            .map { it.path }
            .toList()
    }

    private fun score(set: JankHunterArtifactSet): Int {
        var score = 0
        if (set.ownerMap.isNotBlank()) score += 4
        if (set.classGraph.isNotBlank()) score += 3
        if (set.diagnostics.isNotBlank()) score += 2
        if (set.mapping.isNotBlank()) score += 1
        if (set.diCatalog.isNotBlank()) score += 2
        if (set.variant.contains("debug", ignoreCase = true)) score += 2
        return score
    }

    private fun latestModified(set: JankHunterArtifactSet): Long =
        listOf(set.ownerMap, set.mapping, set.classGraph, set.diagnostics, set.diCatalog)
            .asSequence()
            .filter(String::isNotBlank)
            .mapNotNull { path -> runCatching { Files.getLastModifiedTime(Path.of(path)).toMillis() }.getOrNull() }
            .maxOrNull()
            ?: 0L

    private fun findBuildDirectories(root: File): List<File> {
        val result = mutableListOf<File>()
        root.walkTopDown()
            .maxDepth(MAX_MODULE_SEARCH_DEPTH)
            .onEnter { directory ->
                when {
                    Thread.currentThread().isInterrupted -> false
                    directory != root && directory.name in skippedDirectories -> false
                    directory.name == "build" -> {
                        result += directory
                        false
                    }
                    else -> true
                }
            }
            .forEach { }
        return result
    }

    private fun walkFiles(root: File): Sequence<File> {
        if (!root.isDirectory) return emptySequence()
        return root.walkTopDown()
            .maxDepth(MAX_ARTIFACT_SEARCH_DEPTH)
            .onEnter { !Thread.currentThread().isInterrupted }
            .filter(File::isFile)
    }

    private fun displayVariant(module: String, variant: String): String =
        listOf(module.takeUnless { it == ":" }.orEmpty(), variant)
            .filter(String::isNotBlank)
            .joinToString(":")
            .ifBlank { "detected" }

    private fun File.relativeToOrNull(base: File): File? = runCatching { relativeTo(base) }.getOrNull()

    private class MutableArtifactSet(val variant: String) {
        var ownerMap: String = ""
        var mapping: String = ""
        var classGraph: String = ""
        var diagnostics: String = ""
        var diCatalog: String = ""
    }

    private const val MAX_MODULE_SEARCH_DEPTH = 8
    private const val MAX_ARTIFACT_SEARCH_DEPTH = 6
    private const val MAX_LOG_SEARCH_DEPTH = 10
}
