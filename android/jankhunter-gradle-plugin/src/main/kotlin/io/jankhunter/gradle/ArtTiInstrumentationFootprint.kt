package io.jankhunter.gradle

import java.io.File
import java.io.InputStreamReader
import java.nio.charset.StandardCharsets
import java.util.zip.GZIPInputStream

internal data class ArtTiInstrumentationFootprint(
    val instrumentedClassCount: Int,
    val instrumentedMethodCount: Int,
    val hookCount: Int,
    val instrumentationPassCount: Int,
    val gradleModuleCount: Int,
) {
    val scaleScore: Long
        get() {
            val classes = instrumentedClassCount.coerceAtLeast(0).toLong()
            val methods = (instrumentedMethodCount.coerceAtLeast(0) / 8).toLong()
            val hooks = (hookCount.coerceAtLeast(0) / 4).toLong()
            val modules = (gradleModuleCount.coerceAtLeast(1) * 32).toLong()
            val passes = (instrumentationPassCount.coerceAtLeast(1) * 16).toLong()
            return classes + methods + hooks + modules + passes
        }
}

internal object ArtTiInstrumentationFootprintReader {
    fun read(diagnosticsFile: File?, gradleModuleCount: Int): ArtTiInstrumentationFootprint? {
        val file = diagnosticsFile?.takeIf { it.isFile } ?: return null
        return readFiles(listOf(file), gradleModuleCount)
    }

    fun readFiles(diagnosticsFiles: Iterable<File>, gradleModuleCount: Int): ArtTiInstrumentationFootprint? {
        var classes = 0
        var methods = 0
        var hooks = 0
        val passes = linkedSetOf<String>()
        diagnosticsFiles.filter { it.isFile }.forEach { file ->
            openLineReader(file).forEachLine { line ->
                if (line.isBlank()) return@forEachLine
                classes += 1
                methods += readIntField(line, "methods") ?: 0
                hooks += sumHookCounts(line)
                readQuotedField(line, "pass")?.let(passes::add)
            }
        }
        if (classes == 0) return null
        return ArtTiInstrumentationFootprint(
            instrumentedClassCount = classes,
            instrumentedMethodCount = methods,
            hookCount = hooks,
            instrumentationPassCount = passes.size.coerceAtLeast(1),
            gradleModuleCount = gradleModuleCount.coerceAtLeast(1),
        )
    }

    private fun openLineReader(file: File) = when (file.extension.lowercase()) {
        "gz" -> InputStreamReader(GZIPInputStream(file.inputStream()), StandardCharsets.UTF_8)
        else -> file.reader(StandardCharsets.UTF_8)
    }

    private fun readIntField(line: String, field: String): Int? {
        val pattern = Regex(""""$field":(-?\d+)""")
        val match = pattern.find(line) ?: return null
        return match.groupValues[1].toIntOrNull()
    }

    private fun readQuotedField(line: String, field: String): String? {
        val pattern = Regex(""""$field":"([^"]*)"""")
        return pattern.find(line)?.groupValues?.getOrNull(1)
    }

    private fun sumHookCounts(line: String): Int {
        val hooksStart = line.indexOf("\"hooks\":[")
        if (hooksStart < 0) return 0
        val hooksEnd = line.indexOf("],\"decisions\"", hooksStart)
        if (hooksEnd < hooksStart) return 0
        val section = line.substring(hooksStart, hooksEnd)
        var total = 0
        val countPattern = Regex(""""count":(\d+)""")
        countPattern.findAll(section).forEach { match ->
            total += match.groupValues[1].toIntOrNull() ?: 0
        }
        return total
    }
}
