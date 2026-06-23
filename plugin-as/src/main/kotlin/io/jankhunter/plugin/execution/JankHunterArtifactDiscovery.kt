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
) {
    val isEmpty: Boolean
        get() = ownerMap.isBlank() && mapping.isBlank() && classGraph.isBlank() && diagnostics.isBlank()

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
                    basePath?.resolve("cli/bin/jankhunter"),
                    basePath?.resolve("../cli/bin/jankhunter")?.normalize(),
                    basePath?.resolve("../../cli/bin/jankhunter")?.normalize(),
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

        root.walkTopDown()
            .onEnter { file -> file.name !in skippedDirectories }
            .filter { it.isFile }
            .forEach { file ->
                when (file.name) {
                    "owner-map.json" -> grouped.getOrPut(variantFromGenerated(file), ::MutableArtifactSet).ownerMap = file.path
                    "class-graph.jsonl" -> grouped.getOrPut(variantFromGenerated(file), ::MutableArtifactSet).classGraph = file.path
                    "instrumentation-diagnostics.jsonl" -> {
                        grouped.getOrPut(variantFromGenerated(file), ::MutableArtifactSet).diagnostics = file.path
                    }
                    "mapping.txt" -> {
                        if (file.invariantPath().contains("/build/outputs/mapping/")) {
                            grouped.getOrPut(variantFromMapping(file), ::MutableArtifactSet).mapping = file.path
                        }
                    }
                }
            }

        return grouped
            .map { (variant, mutable) ->
                JankHunterArtifactSet(
                    variant = variant,
                    ownerMap = mutable.ownerMap,
                    mapping = mutable.mapping,
                    classGraph = mutable.classGraph,
                    diagnostics = mutable.diagnostics,
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
            .onEnter { file -> file.name !in skippedDirectories }
            .filter { it.isFile && it.extension == "jhlog" }
            .sortedByDescending { it.lastModified() }
            .take(limit)
            .map { it.path }
            .toList()
    }

    private fun variantFromGenerated(file: File): String {
        val parts = file.invariantPath().split('/')
        val marker = parts.windowed(3).indexOfFirst { it == listOf("build", "generated", "jankhunter") }
        return if (marker >= 0) parts.getOrNull(marker + 3).orEmpty() else ""
    }

    private fun variantFromMapping(file: File): String {
        val parts = file.invariantPath().split('/')
        val marker = parts.windowed(3).indexOfFirst { it == listOf("build", "outputs", "mapping") }
        return if (marker >= 0) parts.getOrNull(marker + 3).orEmpty() else ""
    }

    private fun score(set: JankHunterArtifactSet): Int {
        var score = 0
        if (set.ownerMap.isNotBlank()) score += 4
        if (set.classGraph.isNotBlank()) score += 3
        if (set.diagnostics.isNotBlank()) score += 2
        if (set.mapping.isNotBlank()) score += 1
        if (set.variant.contains("debug", ignoreCase = true)) score += 2
        return score
    }

    private fun latestModified(set: JankHunterArtifactSet): Long =
        listOf(set.ownerMap, set.mapping, set.classGraph, set.diagnostics)
            .asSequence()
            .filter(String::isNotBlank)
            .mapNotNull { path -> runCatching { Files.getLastModifiedTime(Path.of(path)).toMillis() }.getOrNull() }
            .maxOrNull()
            ?: 0L

    private fun File.invariantPath(): String = invariantSeparatorsPath

    private class MutableArtifactSet {
        var ownerMap: String = ""
        var mapping: String = ""
        var classGraph: String = ""
        var diagnostics: String = ""
    }
}
