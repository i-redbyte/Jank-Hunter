package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassWriter

class LifecycleMetadataPathwayTest {
    @Test
    fun lifecycleVisitorRecordsLifecycleMetadataPathway() {
        MetadataReaderMetrics.reset()
        val bytes = LifecycleKotlinProbe::class.java.getResourceAsStream(
            "/${LifecycleKotlinProbe::class.java.name.replace('.', '/')}.class",
        )!!.use { it.readBytes() }
        val reader = ClassReader(bytes)
        val writer = ClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        reader.accept(
            JankHunterClassVisitor(
                writer,
                reader.className,
                lifecycleHookConfig(""),
            ),
            ClassReader.EXPAND_FRAMES,
        )
        assertTrue(MetadataReaderMetrics.pathwayDecodeCount(MetadataDecodePathway.LIFECYCLE) >= 1)
        assertEquals(0, MetadataReaderMetrics.pathwayDecodeCount(MetadataDecodePathway.MAIN))
    }
}

private data class LifecycleKotlinProbe(val id: Int)
