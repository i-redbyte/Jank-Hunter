package io.jankhunter.gradle

import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Test

class JankHunterFeatureContractTest {
    @Test
    fun buildAndRuntimeFeaturesKeepStableNamesAndOrder() {
        val buildFeatures = JankHunterFeature.entries.map(JankHunterFeature::name)
        val runtimeFeatures = runtimeFeatureNames(runtimeFeatureSource())

        assertEquals(EXPECTED_FEATURES, buildFeatures)
        assertEquals(EXPECTED_FEATURES, runtimeFeatures)
    }

    private fun runtimeFeatureSource(): File {
        val start = File(System.getProperty("user.dir")).canonicalFile
        generateSequence(start, File::getParentFile).forEach { directory ->
            val direct = File(
                directory,
                "jankhunter-runtime/src/main/kotlin/io/jankhunter/runtime/JankHunterRuntimeFeature.kt",
            )
            if (direct.isFile) return direct
            val nested = File(
                directory,
                "android/jankhunter-runtime/src/main/kotlin/io/jankhunter/runtime/JankHunterRuntimeFeature.kt",
            )
            if (nested.isFile) return nested
        }
        error("Cannot locate JankHunterRuntimeFeature.kt from $start")
    }

    private fun runtimeFeatureNames(source: File): List<String> {
        val enumBody = source.readText()
            .substringAfter("enum class JankHunterRuntimeFeature {")
            .substringBefore("\n    ;")
        return FEATURE_ENTRY.findAll(enumBody).map { match -> match.groupValues[1] }.toList()
    }

    private companion object {
        val FEATURE_ENTRY = Regex("(?m)^ {4}([A-Z][A-Z0-9_]*),$")
        val EXPECTED_FEATURES = listOf(
            "JANK_STATS",
            "MAIN_LOOPER",
            "SQLITE",
            "ROOM",
            "HTTP",
            "WEBSOCKETS",
            "RUNTIME_IO",
            "BYTECODE_IO",
            "DI_ANALYSIS",
            "HANDLERS",
            "EXECUTORS",
            "COROUTINES",
            "INTERACTIONS",
            "LIFECYCLE_LEAKS",
            "LOGGING",
            "CLASS_GRAPH",
            "CALL_GRAPH",
            "COMPOSE",
            "WORKERS",
            "ANDROID_COMPONENTS",
            "BINDER_IPC",
            "METHOD_COUNTERS",
            "HEAP_DUMPS",
        )
    }
}
