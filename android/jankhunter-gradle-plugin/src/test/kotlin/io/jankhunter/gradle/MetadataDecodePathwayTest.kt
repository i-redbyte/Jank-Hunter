package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassWriter

class MetadataDecodePathwayTest {
    @Test
    fun causalInstrumentationRecordsCausalMetadataPathway() {
        MetadataReaderMetrics.reset()
        val kotlinBytes = PathwayKotlinProbe::class.java.getResourceAsStream(
            "/${PathwayKotlinProbe::class.java.name.replace('.', '/')}.class",
        )!!.use { it.readBytes() }
        val reader = ClassReader(kotlinBytes)
        val writer = ClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        val config = HookConfig(
            methodCounters = false,
            okhttp = false,
            webSockets = false,
            handlers = false,
            executors = false,
            coroutines = false,
            interactionOperations = false,
            logSpam = false,
            classGraph = false,
            runtimeCallGraph = false,
            classGraphDirectory = "",
            instrumentationDiagnosticsDirectory = "",
            metadataDecodePathway = MetadataDecodePathway.CAUSAL,
        )
        reader.accept(
            JankHunterClassVisitor(writer, reader.className, config),
            ClassReader.EXPAND_FRAMES,
        )
        assertTrue(MetadataReaderMetrics.pathwayDecodeCount(MetadataDecodePathway.CAUSAL) >= 1)
        assertEquals(0, MetadataReaderMetrics.pathwayDecodeCount(MetadataDecodePathway.MAIN))
    }
}

private data class PathwayKotlinProbe(val value: Int)
