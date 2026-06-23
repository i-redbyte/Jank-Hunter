package io.jankhunter.plugin.problems

import com.google.gson.JsonArray
import com.google.gson.JsonElement
import com.google.gson.JsonObject
import com.google.gson.JsonParser
import java.io.File

object ProblemsParser {
    fun parse(file: File): ProblemsTable {
        if (!file.isFile) return ProblemsTable(emptyList(), emptyList())
        return when (file.extension.lowercase()) {
            "csv" -> parseCsv(file.readText())
            "json" -> parseJson(file.readText())
            else -> ProblemsTable(emptyList(), emptyList())
        }
    }

    private fun parseCsv(text: String): ProblemsTable {
        val rows = parseCsvRows(text)
        if (rows.isEmpty()) return ProblemsTable(emptyList(), emptyList())
        val header = rows.first()
        val mappedRows = rows.drop(1).map { row ->
            header.mapIndexed { index, name -> name to row.getOrElse(index) { "" } }.toMap()
        }
        return ProblemsTable(header, mappedRows)
    }

    private fun parseJson(text: String): ProblemsTable {
        val root = runCatching { JsonParser.parseString(text) }.getOrNull() ?: return ProblemsTable(emptyList(), emptyList())
        val rows = when {
            root.isJsonArray -> rowsFromArray(root.asJsonArray)
            root.isJsonObject -> rowsFromObject(root.asJsonObject)
            else -> emptyList()
        }
        return tableFromRows(rows)
    }

    private fun rowsFromObject(root: JsonObject): List<Map<String, String>> {
        val knownSections = listOf(
            "top_nodes" to "node",
            "top_edges" to "edge",
            "method_hotspots" to "method",
            "hot_paths" to "path",
            "heuristic" to "finding",
        )
        val rows = mutableListOf<Map<String, String>>()
        for ((section, recordType) in knownSections) {
            val array = root.get(section)?.takeIf(JsonElement::isJsonArray)?.asJsonArray ?: continue
            rows += rowsFromArray(array).map { row -> linkedMapOf("record_type" to recordType) + row }
        }
        if (rows.isNotEmpty()) return rows
        return listOf(flattenObject(root))
    }

    private fun rowsFromArray(array: JsonArray): List<Map<String, String>> =
        array.flatMap { element ->
            when {
                element.isJsonObject -> rowsFromRecord(element.asJsonObject)
                else -> listOf(mapOf("value" to element.asStringSafe()))
            }
        }

    private fun rowsFromRecord(record: JsonObject): List<Map<String, String>> {
        val base = flattenObject(record, skipKeys = setOf("drill_down"))
        val drillDown = record.get("drill_down")?.takeIf(JsonElement::isJsonArray)?.asJsonArray
        if (drillDown == null || drillDown.size() == 0) {
            return listOf(base)
        }
        return drillDown.map { drill ->
            if (drill.isJsonObject) {
                base + flattenObject(drill.asJsonObject)
            } else {
                base + mapOf("drill_down" to drill.asStringSafe())
            }
        }
    }

    private fun flattenObject(
        obj: JsonObject,
        prefix: String = "",
        skipKeys: Set<String> = emptySet(),
    ): Map<String, String> {
        val out = linkedMapOf<String, String>()
        for ((key, value) in obj.entrySet()) {
            if (key in skipKeys) continue
            val name = if (prefix.isBlank()) key else "$prefix.$key"
            when {
                value.isJsonObject -> out += flattenObject(value.asJsonObject, name)
                value.isJsonArray -> out[name] = value.asJsonArray.joinToString(" | ") { it.asStringSafe() }
                else -> out[name] = value.asStringSafe()
            }
        }
        return out
    }

    private fun tableFromRows(rows: List<Map<String, String>>): ProblemsTable {
        val columns = linkedSetOf<String>()
        rows.forEach { columns += it.keys }
        return ProblemsTable(columns.toList(), rows)
    }

    private fun JsonElement.asStringSafe(): String =
        when {
            isJsonNull -> ""
            isJsonPrimitive -> asJsonPrimitive.asString
            isJsonObject -> flattenObject(asJsonObject).entries.joinToString("; ") { "${it.key}=${it.value}" }
            isJsonArray -> asJsonArray.joinToString(" | ") { it.asStringSafe() }
            else -> toString()
        }

    private fun parseCsvRows(text: String): List<List<String>> {
        val rows = mutableListOf<List<String>>()
        val currentRow = mutableListOf<String>()
        val current = StringBuilder()
        var quoted = false
        var index = 0

        while (index < text.length) {
            val char = text[index]
            when {
                quoted && char == '"' && text.getOrNull(index + 1) == '"' -> {
                    current.append('"')
                    index++
                }
                char == '"' -> quoted = !quoted
                char == ',' && !quoted -> {
                    currentRow += current.toString()
                    current.setLength(0)
                }
                (char == '\n' || char == '\r') && !quoted -> {
                    if (char == '\r' && text.getOrNull(index + 1) == '\n') index++
                    currentRow += current.toString()
                    current.setLength(0)
                    if (currentRow.any(String::isNotEmpty)) {
                        rows += currentRow.toList()
                    }
                    currentRow.clear()
                }
                else -> current.append(char)
            }
            index++
        }

        currentRow += current.toString()
        if (currentRow.any(String::isNotEmpty)) {
            rows += currentRow.toList()
        }
        return rows
    }
}
