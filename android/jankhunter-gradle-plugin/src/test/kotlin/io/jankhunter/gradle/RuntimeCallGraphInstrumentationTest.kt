package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes

class RuntimeCallGraphInstrumentationTest {
    @Test
    fun runtimeCallGraphAddsCatchAllExitForExceptionUnwind() {
        val instrumented = instrumentRuntimeCallGraph(throwingFixture())
        val stats = collectMethodStats(instrumented)

        assertEquals(1, stats.parentCatchAllHandlers)
        assertEquals(2, stats.parentExitCalls)
        assertEquals(1, stats.childCatchAllHandlers)
        assertEquals(1, stats.childExitCalls)
    }

    private fun instrumentRuntimeCallGraph(bytes: ByteArray): ByteArray {
        val reader = ClassReader(bytes)
        val writer = ClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        reader.accept(
            JankHunterClassVisitor(
                writer,
                "example.Throwing",
                HookConfig(
                    methodCounters = false,
                    okhttp = false,
                    webSockets = false,
                    handlers = false,
                    executors = false,
                    coroutines = false,
                    flowInteractions = false,
                    logSpam = false,
                    classGraph = false,
                    runtimeCallGraph = true,
                    classGraphPath = "",
                    instrumentationDiagnosticsPath = "",
                ),
            ),
            ClassReader.EXPAND_FRAMES,
        )
        return writer.toByteArray()
    }

    private fun collectMethodStats(bytes: ByteArray): MethodStats {
        val stats = MethodStats()
        ClassReader(bytes).accept(
            object : ClassVisitor(Opcodes.ASM9) {
                override fun visitMethod(
                    access: Int,
                    name: String,
                    descriptor: String,
                    signature: String?,
                    exceptions: Array<out String>?,
                ): MethodVisitor {
                    return object : MethodVisitor(Opcodes.ASM9) {
                        override fun visitTryCatchBlock(
                            start: org.objectweb.asm.Label,
                            end: org.objectweb.asm.Label,
                            handler: org.objectweb.asm.Label,
                            type: String?,
                        ) {
                            if (type != null) return
                            when (name) {
                                "parent" -> stats.parentCatchAllHandlers++
                                "child" -> stats.childCatchAllHandlers++
                            }
                        }

                        override fun visitMethodInsn(
                            opcodeAndSource: Int,
                            owner: String,
                            methodName: String,
                            descriptor: String,
                            isInterface: Boolean,
                        ) {
                            if (owner != "io/jankhunter/runtime/JankHunter" || methodName != "exitMethod") return
                            when (name) {
                                "parent" -> stats.parentExitCalls++
                                "child" -> stats.childExitCalls++
                            }
                        }
                    }
                }
            },
            0,
        )
        return stats
    }

    private fun throwingFixture(): ByteArray {
        val writer = ClassWriter(ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "example/Throwing", null, "java/lang/Object", null)

        writer.visitMethod(Opcodes.ACC_PUBLIC, "parent", "()V", null, null).apply {
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            visitMethodInsn(Opcodes.INVOKEVIRTUAL, "example/Throwing", "child", "()V", false)
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }

        writer.visitMethod(Opcodes.ACC_PUBLIC, "child", "()V", null, null).apply {
            visitCode()
            visitTypeInsn(Opcodes.NEW, "java/lang/RuntimeException")
            visitInsn(Opcodes.DUP)
            visitMethodInsn(Opcodes.INVOKESPECIAL, "java/lang/RuntimeException", "<init>", "()V", false)
            visitInsn(Opcodes.ATHROW)
            visitMaxs(0, 0)
            visitEnd()
        }

        writer.visitEnd()
        return writer.toByteArray()
    }

    private data class MethodStats(
        var parentCatchAllHandlers: Int = 0,
        var parentExitCalls: Int = 0,
        var childCatchAllHandlers: Int = 0,
        var childExitCalls: Int = 0,
    )
}
