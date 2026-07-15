package io.jankhunter.gradle

import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.AnnotationVisitor
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Label
import org.objectweb.asm.Opcodes
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
