package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Handle
import org.objectweb.asm.Opcodes
import org.objectweb.asm.Type
import org.objectweb.asm.tree.ClassNode
import org.objectweb.asm.tree.MethodInsnNode
import org.objectweb.asm.tree.LdcInsnNode

class TaskLambdaInstrumentationTest {
    @Test
    fun metafactoryProofCoversSyntheticTaskBodyEvenBeforeCreationSite() {
        assertEquals(1, executionHooks("java/lang/Runnable"))
        assertEquals(1, executionHooks("java/lang/Runnable", dottedClassName = true))
        assertEquals(0, executionHooks("test/OtherFunction"))
    }

    @Test
    fun callableAndClickCallbackBodiesHaveShortExecutionHooks() {
        assertEquals(1, executionHooks("java/util/concurrent/Callable", "call", "()Ljava/lang/Object;", "enterCallableCall"))
        assertEquals(1, executionHooks("android/view/View\$OnClickListener", "onClick", "(Landroid/view/View;)V"))
    }

    private fun executionHooks(
        result: String, samName: String = "run", bodyDescriptor: String = "()V", hook: String = "enterRunnableRun",
        dottedClassName: Boolean = false,
    ): Int {
        val source = ClassWriter(ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        source.visit(Opcodes.V1_8, Opcodes.ACC_PUBLIC, "test/Lambdas", null, "java/lang/Object", null)
        source.visitMethod(Opcodes.ACC_PRIVATE or Opcodes.ACC_STATIC or Opcodes.ACC_SYNTHETIC, "body", bodyDescriptor, null, null).apply {
            visitCode()
            if (Type.getReturnType(bodyDescriptor) == Type.VOID_TYPE) visitInsn(Opcodes.RETURN)
            else {
                visitInsn(Opcodes.ACONST_NULL)
                visitInsn(Opcodes.ARETURN)
            }
            visitMaxs(0, 0)
            visitEnd()
        }
        source.visitMethod(Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC, "create", "()L$result;", null, null).apply {
            visitCode()
            visitInvokeDynamicInsn(samName, "()L$result;", Handle(Opcodes.H_INVOKESTATIC,
                "java/lang/invoke/LambdaMetafactory", "metafactory",
                "(Ljava/lang/invoke/MethodHandles\$Lookup;Ljava/lang/String;Ljava/lang/invoke/MethodType;" +
                    "Ljava/lang/invoke/MethodType;Ljava/lang/invoke/MethodHandle;Ljava/lang/invoke/MethodType;)Ljava/lang/invoke/CallSite;", false),
                Type.getMethodType(bodyDescriptor), Handle(Opcodes.H_INVOKESTATIC, "test/Lambdas", "body", bodyDescriptor, false),
                Type.getMethodType(bodyDescriptor))
            visitInsn(Opcodes.ARETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        source.visitEnd()
        val reader = ClassReader(source.toByteArray())
        val output = ClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        val config = HookConfig(methodCounters = true, okhttp = false, webSockets = false, handlers = false,
            executors = true, concurrencyDiagnostics = true, coroutines = false, interactionOperations = false,
            logSpam = false, classGraph = false, runtimeCallGraph = false, classGraphDirectory = "",
            instrumentationDiagnosticsDirectory = "", databaseTracing = false)
        reader.accept(JankHunterClassVisitor(output,
            if (dottedClassName) reader.className.replace('/', '.') else reader.className, config), ClassReader.EXPAND_FRAMES)
        val node = ClassNode()
        ClassReader(output.toByteArray()).accept(node, 0)
        assertTrue(node.methods.single { it.name == "body" }.instructions.toArray().filterIsInstance<LdcInsnNode>()
            .mapNotNull { it.cst as? Type }.none { '.' in it.internalName })
        return node.methods.single { it.name == "body" }.instructions.toArray().filterIsInstance<MethodInsnNode>()
            .count { it.owner == "io/jankhunter/runtime/JankHunterHooks" && it.name == hook }
    }
}
