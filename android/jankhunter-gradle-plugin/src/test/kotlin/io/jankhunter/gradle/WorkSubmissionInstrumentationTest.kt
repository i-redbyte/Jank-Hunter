package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes
import org.objectweb.asm.Type

class WorkSubmissionInstrumentationTest {
    @Test
    fun publicBuilderCallsKeepOriginalResultAndCaptureExplicitHintsOnlyWhenEnabled() {
        val signatures = listOf(
            Triple("setBackoffCriteria", "(Landroidx/work/BackoffPolicy;JLjava/util/concurrent/TimeUnit;)Landroidx/work/WorkRequest\$Builder;", "observeWorkBuilderBackoffCall"),
            Triple("setBackoffCriteria", "(Landroidx/work/BackoffPolicy;Ljava/time/Duration;)Landroidx/work/WorkRequest\$Builder;", "observeWorkBuilderBackoffDurationCall"),
            Triple("setExpedited", "(Landroidx/work/OutOfQuotaPolicy;)Landroidx/work/WorkRequest\$Builder;", "observeWorkBuilderExpeditedCall"),
            Triple("build", "()Landroidx/work/WorkRequest;", "observeWorkBuilderBuildCall"),
        )
        signatures.forEach { (name, descriptor, hook) ->
            assertEquals(listOf(name, hook), builderCalls(name, descriptor, true))
            assertEquals(listOf(name), builderCalls(name, descriptor, false))
            assertEquals(listOf(name, hook), builderCalls(name, descriptor, true, "androidx/work/OneTimeWorkRequest\$Builder"))
        }
    }

    private fun builderCalls(name: String, descriptor: String, enabled: Boolean,
                             owner: String = "androidx/work/WorkRequest\$Builder"): List<String> {
        val arguments = Type.getArgumentTypes(descriptor)
        val original = ClassWriter(0)
        original.visit(Opcodes.V1_8, Opcodes.ACC_PUBLIC, "example/BuilderCaller", null, "java/lang/Object", null)
        original.visitMethod(Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC, "call", "(L$owner;" + descriptor.substring(1), null, null).apply {
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            var slot = 1
            arguments.forEach { type ->
                visitVarInsn(type.getOpcode(Opcodes.ILOAD), slot)
                slot += type.size
            }
            visitMethodInsn(Opcodes.INVOKEVIRTUAL, owner, name, descriptor, false)
            visitInsn(Opcodes.ARETURN)
            visitMaxs(6, slot)
            visitEnd()
        }
        original.visitEnd()
        val reader = ClassReader(original.toByteArray())
        val writer = ClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        reader.accept(JankHunterClassVisitor(writer, reader.className, HookConfig(
            methodCounters = false, okhttp = false, webSockets = false, handlers = false,
            executors = false, coroutines = false, interactionOperations = false, logSpam = false,
            classGraph = false, runtimeCallGraph = false, classGraphDirectory = "", instrumentationDiagnosticsDirectory = "",
            workerTracing = enabled,
        )), ClassReader.EXPAND_FRAMES)
        val result = mutableListOf<String>()
        ClassReader(writer.toByteArray()).accept(object : ClassVisitor(Opcodes.ASM9) {
            override fun visitMethod(access: Int, name: String, descriptor: String, signature: String?, exceptions: Array<out String>?): MethodVisitor =
                object : MethodVisitor(Opcodes.ASM9) {
                    override fun visitMethodInsn(opcode: Int, owner: String, name: String, descriptor: String, isInterface: Boolean) {
                        if (owner.startsWith("androidx/work/") || name.startsWith("observeWorkBuilder")) result += name
                    }
                }
        }, 0)
        return result
    }

    @Test
    fun ordinaryWorkManagerCallsKeepOriginalInvocationBeforeBoundedObservation() {
        for ((name, descriptor) in signatures) {
            assertEquals(listOf(name, "observeWorkSubmissionRequests"), calls(name, descriptor, true))
            assertEquals(listOf(name), calls(name, descriptor, false))
        }
    }

    private fun calls(name: String, descriptor: String, enabled: Boolean): List<String> {
        val original = ClassWriter(0)
        original.visit(Opcodes.V1_8, Opcodes.ACC_PUBLIC, "example/EnqueueCaller", null, "java/lang/Object", null)
        original.visitMethod(Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC, "submit",
            "(Landroidx/work/WorkManager;" + descriptor.substring(1), null, null).apply {
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            Type.getArgumentTypes(descriptor).indices.forEach { visitVarInsn(Opcodes.ALOAD, it + 1) }
            visitMethodInsn(Opcodes.INVOKEVIRTUAL, "androidx/work/WorkManager", name, descriptor, false)
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
                        if (owner == "androidx/work/WorkManager" || name == "observeWorkSubmissionRequests") result += name
                    }
                }
        }, 0)
        return result
    }

    private val signatures = listOf(
        "enqueue" to "(Landroidx/work/WorkRequest;)Landroidx/work/Operation;",
        "enqueue" to "(Ljava/util/List;)Landroidx/work/Operation;",
        "enqueueUniqueWork" to "(Ljava/lang/String;Landroidx/work/ExistingWorkPolicy;Landroidx/work/OneTimeWorkRequest;)Landroidx/work/Operation;",
        "enqueueUniqueWork" to "(Ljava/lang/String;Landroidx/work/ExistingWorkPolicy;Ljava/util/List;)Landroidx/work/Operation;",
        "enqueueUniquePeriodicWork" to "(Ljava/lang/String;Landroidx/work/ExistingPeriodicWorkPolicy;Landroidx/work/PeriodicWorkRequest;)Landroidx/work/Operation;",
    )
}
