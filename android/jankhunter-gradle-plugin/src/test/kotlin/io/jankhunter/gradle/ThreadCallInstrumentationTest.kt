package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes

class ThreadCallInstrumentationTest {
    @Test fun recordsStartAndFactoryOnlyAfterTheirOriginalCalls() {
        assertEquals(listOf("start", "observeThreadStart", "newThread", "observeThreadFactory"), calls(true))
        assertEquals(listOf("start", "newThread"), calls(false))
    }

    private fun calls(enabled: Boolean): List<String> {
        val original = ClassWriter(0)
        original.visit(Opcodes.V1_8, Opcodes.ACC_PUBLIC, "example/Caller", null, "java/lang/Object", null)
        original.visitMethod(Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC, "launch", "(Ljava/lang/Thread;)V", null, null).apply {
            visitCode(); visitVarInsn(Opcodes.ALOAD, 0)
            visitMethodInsn(Opcodes.INVOKEVIRTUAL, "java/lang/Thread", "start", "()V", false)
            visitInsn(Opcodes.RETURN); visitMaxs(1, 1); visitEnd()
        }
        original.visitMethod(Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC, "create", "(Ljava/util/concurrent/ThreadFactory;Ljava/lang/Runnable;)Ljava/lang/Thread;", null, null).apply {
            visitCode(); visitVarInsn(Opcodes.ALOAD, 0); visitVarInsn(Opcodes.ALOAD, 1)
            visitMethodInsn(Opcodes.INVOKEINTERFACE, "java/util/concurrent/ThreadFactory", "newThread", "(Ljava/lang/Runnable;)Ljava/lang/Thread;", true)
            visitInsn(Opcodes.ARETURN); visitMaxs(2, 2); visitEnd()
        }
        original.visitEnd()
        val reader = ClassReader(original.toByteArray())
        val writer = ClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        val config = HookConfig(methodCounters = false, okhttp = false, webSockets = false, handlers = false,
            executors = enabled, coroutines = false, interactionOperations = false, logSpam = false,
            classGraph = false, runtimeCallGraph = false, classGraphDirectory = "", instrumentationDiagnosticsDirectory = "")
        reader.accept(JankHunterClassVisitor(writer, reader.className, config), ClassReader.EXPAND_FRAMES)
        val result = mutableListOf<String>()
        ClassReader(writer.toByteArray()).accept(object : ClassVisitor(Opcodes.ASM9) {
            override fun visitMethod(access: Int, name: String, descriptor: String, signature: String?, exceptions: Array<out String>?): MethodVisitor =
                object : MethodVisitor(Opcodes.ASM9) {
                    override fun visitMethodInsn(opcode: Int, owner: String, name: String, descriptor: String, isInterface: Boolean) {
                        if (owner in setOf("java/lang/Thread", "java/util/concurrent/ThreadFactory", "io/jankhunter/runtime/JankHunterHooks")) result += name
                    }
                }
        }, 0)
        return result
    }
}
