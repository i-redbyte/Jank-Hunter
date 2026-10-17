package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes

class ThreadRunInstrumentationTest {
    @Test fun instrumentsOnlyExecutingRunnableOrThreadBody() {
        assertEquals(1, hookCount("java/lang/Runnable", true))
        assertEquals(1, hookCount("java/lang/Thread", true))
        assertEquals(0, hookCount("example/Ordinary", true))
        assertEquals(0, hookCount("java/lang/Runnable", false))
        assertEquals(0, hookCount("java/lang/Runnable", true, Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC))
    }

    private fun hookCount(parent: String, enabled: Boolean, access: Int = Opcodes.ACC_PUBLIC): Int {
        val fixture = ClassWriter(0)
        fixture.visit(Opcodes.V1_8, Opcodes.ACC_PUBLIC, "example/Task", null, "java/lang/Object", null)
        fixture.visitMethod(access, "run", "()V", null, null).apply {
            visitCode()
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 1)
            visitEnd()
        }
        fixture.visitEnd()
        val reader = ClassReader(fixture.toByteArray())
        val writer = ClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        val config = HookConfig(
            methodCounters = false, okhttp = false, webSockets = false, handlers = false,
            executors = enabled, coroutines = false, interactionOperations = false, logSpam = false,
            classGraph = false, runtimeCallGraph = false, classGraphDirectory = "", instrumentationDiagnosticsDirectory = "",
        )
        reader.accept(JankHunterClassVisitor(writer, reader.className, config, classHierarchy = setOf(parent)), ClassReader.EXPAND_FRAMES)
        var count = 0
        ClassReader(writer.toByteArray()).accept(object : ClassVisitor(Opcodes.ASM9) {
            override fun visitMethod(access: Int, name: String, descriptor: String, signature: String?, exceptions: Array<out String>?): MethodVisitor {
                return object : MethodVisitor(Opcodes.ASM9) {
                    override fun visitMethodInsn(opcode: Int, owner: String, name: String, descriptor: String, isInterface: Boolean) {
                        if (owner == "io/jankhunter/runtime/JankHunterHooks" && name == "observeExecutingThread") count++
                    }
                }
            }
        }, 0)
        return count
    }
}
