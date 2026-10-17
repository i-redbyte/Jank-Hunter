package io.jankhunter.gradle

import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Opcodes

class InstrumentationTransformIntegrityTest {
    @Test
    fun strictMetadataFailureOccursBeforeInstrumentationMarkerIsEmitted() {
        val writer = ClassWriter(0)
        writer.visit(Opcodes.V11, Opcodes.ACC_PUBLIC, "fixture/BrokenKotlin", null, "java/lang/Object", null)
        writer.visitAnnotation(KotlinGeneratedMethodIndex.METADATA_DESCRIPTOR, true).apply {
            visit("k", 1)
            visit("mv", intArrayOf(99, 0))
            visitEnd()
        }
        writer.visitEnd()
        val bytes = writer.toByteArray()
        val output = ClassWriter(ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        val config = HookConfig(
            methodCounters = true,
            okhttp = false,
            webSockets = false,
            handlers = false,
            executors = false,
            coroutines = false,
            interactionOperations = false,
            logSpam = false,
            classGraph = false,
            runtimeCallGraph = true,
            classGraphDirectory = "",
            instrumentationDiagnosticsDirectory = "",
            metadataCompatibilityMode = JankHunterMetadataCompatibilityMode.STRICT,
        )
        assertThrows(IllegalStateException::class.java) {
            ClassReader(bytes).accept(JankHunterClassVisitor(output, "fixture/BrokenKotlin", config), ClassReader.EXPAND_FRAMES)
        }
        assertFalse(String(output.toByteArray(), Charsets.ISO_8859_1).contains("JankHunterInstrumented"))
    }
}
