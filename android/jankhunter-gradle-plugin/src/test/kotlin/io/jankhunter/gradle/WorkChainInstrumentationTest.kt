package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes
import org.objectweb.asm.Type

class WorkChainInstrumentationTest {
    @Test fun rootsAndChildrenAreObservedOnlyAfterOriginalCallReturns() {
        for ((owner, name, descriptor, hook) in signatures) {
            assertEquals(listOf(name, hook), calls(owner, name, descriptor, true))
            assertEquals(listOf(name), calls(owner, name, descriptor, false))
        }
    }

    private fun calls(owner: String, name: String, descriptor: String, enabled: Boolean): List<String> {
        val original = ClassWriter(0)
        original.visit(Opcodes.V1_8, Opcodes.ACC_PUBLIC, "example/ChainCaller", null, "java/lang/Object", null)
        original.visitMethod(Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC, "chain",
            "(L$owner;" + descriptor.substring(1), null, null).apply {
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            Type.getArgumentTypes(descriptor).indices.forEach { visitVarInsn(Opcodes.ALOAD, it + 1) }
            visitMethodInsn(Opcodes.INVOKEVIRTUAL, owner, name, descriptor, false)
            visitInsn(Opcodes.ARETURN)
            visitMaxs(4, 4)
            visitEnd()
        }
        original.visitEnd()
        val reader = ClassReader(original.toByteArray())
        val writer = ClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        val config = HookConfig(methodCounters = false, okhttp = false, webSockets = false, handlers = false,
            executors = false, coroutines = false, interactionOperations = false, logSpam = false,
            classGraph = false, runtimeCallGraph = false, classGraphDirectory = "", instrumentationDiagnosticsDirectory = "",
            workerTracing = enabled)
        reader.accept(JankHunterClassVisitor(writer, reader.className, config), ClassReader.EXPAND_FRAMES)
        val result = mutableListOf<String>()
        ClassReader(writer.toByteArray()).accept(object : ClassVisitor(Opcodes.ASM9) {
            override fun visitMethod(access: Int, name: String, descriptor: String, signature: String?, exceptions: Array<out String>?): MethodVisitor =
                object : MethodVisitor(Opcodes.ASM9) {
                    override fun visitMethodInsn(opcode: Int, owner: String, name: String, descriptor: String, isInterface: Boolean) {
                        if (owner == "androidx/work/WorkManager" || owner == "androidx/work/WorkContinuation" ||
                            name == "observeWorkChainRootRequests" || name == "observeWorkChainChildRequests") result += name
                    }
                }
        }, 0)
        return result
    }

    private val signatures = listOf(
        listOf("androidx/work/WorkManager", "beginWith", "(Landroidx/work/OneTimeWorkRequest;)Landroidx/work/WorkContinuation;", "observeWorkChainRootRequests"),
        listOf("androidx/work/WorkManager", "beginWith", "(Ljava/util/List;)Landroidx/work/WorkContinuation;", "observeWorkChainRootRequests"),
        listOf("androidx/work/WorkManager", "beginUniqueWork", "(Ljava/lang/String;Landroidx/work/ExistingWorkPolicy;Landroidx/work/OneTimeWorkRequest;)Landroidx/work/WorkContinuation;", "observeWorkChainRootRequests"),
        listOf("androidx/work/WorkManager", "beginUniqueWork", "(Ljava/lang/String;Landroidx/work/ExistingWorkPolicy;Ljava/util/List;)Landroidx/work/WorkContinuation;", "observeWorkChainRootRequests"),
        listOf("androidx/work/WorkContinuation", "then", "(Landroidx/work/OneTimeWorkRequest;)Landroidx/work/WorkContinuation;", "observeWorkChainChildRequests"),
        listOf("androidx/work/WorkContinuation", "then", "(Ljava/util/List;)Landroidx/work/WorkContinuation;", "observeWorkChainChildRequests"),
    )
}
