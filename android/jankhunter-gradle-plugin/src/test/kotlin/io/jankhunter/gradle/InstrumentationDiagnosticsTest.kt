package io.jankhunter.gradle

import org.junit.Assert.assertTrue
import org.junit.Assert.assertFalse
import org.junit.Test
import org.objectweb.asm.AnnotationVisitor
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.Label
import org.objectweb.asm.Opcodes
import org.objectweb.asm.MethodVisitor
import java.nio.file.Files

class InstrumentationDiagnosticsTest {
    @Test
    fun classVisitorWritesInstrumentationDiagnosticsJsonl() {
        val diagnostics = Files.createTempDirectory("jankhunter-diagnostics").toFile()

        val reader = ClassReader(fixture())
        val writer = ClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        reader.accept(
            JankHunterClassVisitor(
                writer,
                "example/Diagnostics",
                HookConfig(
                    methodCounters = false,
                    okhttp = false,
                    webSockets = false,
                    handlers = false,
                    executors = false,
                    coroutines = true,
                    flowInteractions = false,
                    logSpam = true,
                    classGraph = false,
                    runtimeCallGraph = false,
                    classGraphDirectory = "",
                    instrumentationDiagnosticsDirectory = diagnostics.absolutePath,
                    ownerMapEntriesDirectory = "",
                ),
            ),
            0,
        )

        val text = InstrumentationArtifactFiles.readJsonlLines(diagnostics).joinToString("\n")
        assertTrue(text.contains("\"format\":1"))
        assertTrue(text.contains("\"class\":\"example.Diagnostics\""))
        assertTrue(text.contains("\"methods\":1"))
        assertTrue(text.contains("\"annotatedMethods\":1"))
        assertTrue(text.contains("\"methodFilterIncluded\":1"))
        assertTrue(text.contains("\"methodFilterExcluded\":0"))
        assertTrue(text.contains("\"reason\":\"included:regular\""))
        assertTrue(text.contains("\"intent\":\"logspam.android.util.Log.d\""))
        assertTrue(text.contains("\"signature\":\"logspam.android.util.Log.d\""))
        assertTrue(text.contains("\"method\":\"load()V\""))
        assertTrue(text.contains("\"line\":42"))
        assertTrue(text.contains("\"reason\":\"near_miss_coroutine_signature\""))
        assertTrue(text.contains("\"line\":55"))
        assertTrue(text.contains("\"owner\":\"FeedOwner\""))
        assertTrue(text.contains("\"screen\":\"FeedScreen\""))
        assertTrue(text.contains("\"flow\":\"feed.open\""))
        assertTrue(text.contains("\"trace\":\"refresh\""))
    }

    @Test
    fun enabledFilterRemovesOnlyBoundaryHooksAndKeepsCallSiteHooks() {
        val diagnostics = Files.createTempDirectory("jankhunter-filter-diagnostics").toFile()
        val reader = ClassReader(syntheticLogFixture())
        val writer = ClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        reader.accept(
            JankHunterClassVisitor(
                writer,
                "example/Filtered",
                HookConfig(
                    methodCounters = true,
                    methodFilterMode = JankHunterMethodFilterMode.ENABLED,
                    okhttp = false,
                    webSockets = false,
                    handlers = false,
                    executors = false,
                    coroutines = false,
                    flowInteractions = false,
                    logSpam = true,
                    classGraph = false,
                    runtimeCallGraph = true,
                    classGraphDirectory = "",
                    instrumentationDiagnosticsDirectory = diagnostics.absolutePath,
                    ownerMapEntriesDirectory = "",
                ),
            ),
            0,
        )

        val calls = jankHunterHookCalls(writer.toByteArray())
        assertFalse("recordMethodCall" in calls)
        assertFalse("enterMethod" in calls)
        assertFalse("exitMethod" in calls)
        assertTrue("recordLogSpam" in calls)
        val text = InstrumentationArtifactFiles.readJsonlLines(diagnostics).joinToString("\n")
        assertTrue(text.contains("\"methodFilterExcluded\":1"))
        assertTrue(text.contains("\"reason\":\"excluded:acc_synthetic\""))
    }

    private fun syntheticLogFixture(): ByteArray {
        val writer = ClassWriter(ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "example/Filtered", null, "java/lang/Object", null)
        writer.visitMethod(Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC or Opcodes.ACC_SYNTHETIC, "access\$200", "()V", null, null).run {
            visitCode()
            visitLdcInsn("tag")
            visitLdcInsn("message")
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                "android/util/Log",
                "d",
                "(Ljava/lang/String;Ljava/lang/String;)I",
                false,
            )
            visitInsn(Opcodes.POP)
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun jankHunterHookCalls(bytecode: ByteArray): Set<String> {
        val result = linkedSetOf<String>()
        ClassReader(bytecode).accept(object : ClassVisitor(Opcodes.ASM9) {
            override fun visitMethod(
                access: Int,
                name: String?,
                descriptor: String?,
                signature: String?,
                exceptions: Array<out String>?,
            ): MethodVisitor {
                return object : MethodVisitor(Opcodes.ASM9) {
                    override fun visitMethodInsn(
                        opcode: Int,
                        owner: String,
                        name: String,
                        descriptor: String,
                        isInterface: Boolean,
                    ) {
                        if (owner == "io/jankhunter/runtime/JankHunterHooks") result += name
                    }
                }
            }
        }, 0)
        return result
    }

    private fun fixture(): ByteArray {
        val writer = ClassWriter(ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "example/Diagnostics", null, "java/lang/Object", null)
        writer.visitAnnotation(OWNER_DESCRIPTOR, false).stringValue("FeedOwner")
        writer.visitAnnotation(SCREEN_DESCRIPTOR, false).stringValue("FeedScreen")
        writer.visitAnnotation(FLOW_DESCRIPTOR, false).stringValue("feed.open")
        writer.visitMethod(Opcodes.ACC_PUBLIC, "load", "()V", null, null).run {
            visitAnnotation(TRACE_DESCRIPTOR, false).stringValue("refresh")
            visitCode()
            val logLine = Label()
            visitLabel(logLine)
            visitLineNumber(42, logLine)
            visitLdcInsn("JankHunter")
            visitLdcInsn("diagnostics")
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                "android/util/Log",
                "d",
                "(Ljava/lang/String;Ljava/lang/String;)I",
                false,
            )
            visitInsn(Opcodes.POP)
            val coroutineLine = Label()
            visitLabel(coroutineLine)
            visitLineNumber(55, coroutineLine)
            visitInsn(Opcodes.ACONST_NULL)
            visitInsn(Opcodes.ACONST_NULL)
            visitInsn(Opcodes.ACONST_NULL)
            visitInsn(Opcodes.ACONST_NULL)
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                "kotlinx/coroutines/BuildersKt__BuildersKt",
                "launch",
                "(Lkotlinx/coroutines/CoroutineScope;Lkotlin/coroutines/CoroutineContext;" +
                    "Lkotlinx/coroutines/CoroutineStart;Lkotlin/jvm/functions/Function2;)Ljava/lang/Object;",
                false,
            )
            visitInsn(Opcodes.POP)
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun AnnotationVisitor.stringValue(value: String) {
        visit("value", value)
        visitEnd()
    }

    private companion object {
        private const val OWNER_DESCRIPTOR = "Lio/jankhunter/annotations/JankHunterOwner;"
        private const val SCREEN_DESCRIPTOR = "Lio/jankhunter/annotations/JankHunterScreen;"
        private const val FLOW_DESCRIPTOR = "Lio/jankhunter/annotations/JankHunterFlow;"
        private const val TRACE_DESCRIPTOR = "Lio/jankhunter/annotations/JankHunterTrace;"
    }
}
