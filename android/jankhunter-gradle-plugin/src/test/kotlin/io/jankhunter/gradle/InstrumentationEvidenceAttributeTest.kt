package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Opcodes

class InstrumentationEvidenceAttributeTest {
    @Test fun laterPassPreservesPrimaryComponentCoverage() {
        val source = ClassWriter(0).apply {
            visit(Opcodes.V1_8, Opcodes.ACC_PUBLIC, "app/Service", null, "java/lang/Object", null)
            visitAttribute(InstrumentationEvidenceAttribute("android_component", "primary-component-record"))
            visitEnd()
        }.toByteArray()
        val writer = ClassWriter(0)
        ClassReader(source).accept(JankHunterClassVisitor(writer, "app.Service",
            lifecycleHookConfig("").copy(lifecycleLeaks = false,
                metadataDecodePathway = MetadataDecodePathway.CAUSAL),
            classHierarchy = setOf("android/app/Service")), 0)
        val root = kotlin.io.path.createTempDirectory("build-evidence").toFile()
        try {
            root.resolve("Service.class").writeBytes(writer.toByteArray())
            val records = ArrayList<String>()
            InstrumentationEvidenceAttribute.forEachRecord(emptyList(), listOf(root)) { kind, json ->
                if (kind == "android_component") records += json
            }
            assertEquals(listOf("primary-component-record"), records)
        } finally { root.deleteRecursively() }
    }
}
