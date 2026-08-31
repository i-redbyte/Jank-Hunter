package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes

class AndroidBroadcastReceiverInstrumentationTest {
    @Test
    fun policyMatchesExactReceiverAndPendingResultApis() {
        assertEquals(
            AndroidReceiverInvocation.RECEIVE,
            AndroidBroadcastReceiverInstrumentationPolicy.callback(
                "onReceive",
                "(Landroid/content/Context;Landroid/content/Intent;)V",
            ),
        )
        assertEquals(
            AndroidReceiverInvocation.GO_ASYNC,
            AndroidBroadcastReceiverInstrumentationPolicy.invocation(
                ANDROID_RECEIVER,
                "goAsync",
                "()Landroid/content/BroadcastReceiver${'$'}PendingResult;",
            ),
        )
        assertEquals(
            AndroidReceiverInvocation.GO_ASYNC,
            AndroidBroadcastReceiverInstrumentationPolicy.invocation(
                "example/BootReceiver",
                "goAsync",
                "()Landroid/content/BroadcastReceiver${'$'}PendingResult;",
                setOf("example/BootReceiver", ANDROID_RECEIVER),
            ),
        )
        assertEquals(
            AndroidReceiverInvocation.FINISH_ASYNC,
            AndroidBroadcastReceiverInstrumentationPolicy.invocation(
                ANDROID_PENDING_RESULT,
                "finish",
                "()V",
            ),
        )
        assertNull(AndroidBroadcastReceiverInstrumentationPolicy.callback("onReceive", "()V"))
        assertNull(AndroidBroadcastReceiverInstrumentationPolicy.invocation(ANDROID_PENDING_RESULT, "finish", "(I)V"))
    }

    @Test
    fun receiverAndGoAsyncHaveBalancedHooksAndFinishRunsFirst() {
        val calls = instrumentAndCollect(receiverFixture())

        assertEquals(1, calls.getValue("onReceive").enters)
        assertEquals(2, calls.getValue("onReceive").exits)
        assertEquals(1, calls.getValue("onReceive").asyncRegistrations)
        assertEquals(
            listOf("framework:finish", "hook:finish"),
            calls.getValue("complete").finishOrder,
        )
    }

    private fun instrumentAndCollect(bytes: ByteArray): Map<String, ReceiverCalls> {
        val reader = ClassReader(bytes)
        val writer = object : ClassWriter(reader, COMPUTE_FRAMES or COMPUTE_MAXS) {
            override fun getCommonSuperClass(type1: String?, type2: String?): String = "java/lang/Object"
        }
        reader.accept(
            JankHunterClassVisitor(
                writer,
                reader.className,
                config(),
                classHierarchy = setOf(reader.className, ANDROID_RECEIVER),
                resolveOwnerHierarchy = { owner -> setOf(owner) },
            ),
            ClassReader.EXPAND_FRAMES,
        )
        val calls = linkedMapOf<String, ReceiverCalls>()
        ClassReader(writer.toByteArray()).accept(
            object : ClassVisitor(Opcodes.ASM9) {
                override fun visitMethod(
                    access: Int,
                    name: String,
                    descriptor: String,
                    signature: String?,
                    exceptions: Array<out String>?,
                ): MethodVisitor {
                    return object : MethodVisitor(Opcodes.ASM9) {
                        override fun visitMethodInsn(
                            opcodeAndSource: Int,
                            owner: String,
                            methodName: String,
                            methodDescriptor: String,
                            isInterface: Boolean,
                        ) {
                            val stats = calls.getOrPut(name, ::ReceiverCalls)
                            if (owner == ANDROID_PENDING_RESULT && methodName == "finish") {
                                stats.finishOrder += "framework:finish"
                            }
                            if (owner != JANK_HUNTER_ANDROID_HOOKS) return
                            when (methodName) {
                                "enterReceiverCallback" -> stats.enters++
                                "exitReceiverCallback" -> stats.exits++
                                "registerReceiverAsync" -> stats.asyncRegistrations++
                                "finishReceiverAsync" -> stats.finishOrder += "hook:finish"
                            }
                        }
                    }
                }
            },
            0,
        )
        return calls.filterValues { value ->
            value.enters != 0 || value.exits != 0 || value.asyncRegistrations != 0 || value.finishOrder.isNotEmpty()
        }
    }

    private fun receiverFixture(): ByteArray {
        val writer = ClassWriter(ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "example/BootReceiver", null, ANDROID_RECEIVER, null)
        writer.method(
            Opcodes.ACC_PUBLIC,
            "onReceive",
            "(Landroid/content/Context;Landroid/content/Intent;)V",
        ) {
            visitVarInsn(Opcodes.ALOAD, 0)
            visitMethodInsn(
                Opcodes.INVOKEVIRTUAL,
                ANDROID_RECEIVER,
                "goAsync",
                "()Landroid/content/BroadcastReceiver${'$'}PendingResult;",
                false,
            )
            visitInsn(Opcodes.POP)
            visitInsn(Opcodes.RETURN)
        }
        writer.method(Opcodes.ACC_PUBLIC, "complete", "(Landroid/content/BroadcastReceiver${'$'}PendingResult;)V") {
            visitVarInsn(Opcodes.ALOAD, 1)
            visitMethodInsn(Opcodes.INVOKEVIRTUAL, ANDROID_PENDING_RESULT, "finish", "()V", false)
            visitInsn(Opcodes.RETURN)
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun ClassWriter.method(
        access: Int,
        name: String,
        descriptor: String,
        body: MethodVisitor.() -> Unit,
    ) {
        visitMethod(access, name, descriptor, null, null).apply {
            visitCode()
            body()
            visitMaxs(0, 0)
            visitEnd()
        }
    }

    private fun config(): HookConfig = HookConfig(
        methodCounters = false,
        methodFilterMode = JankHunterMethodFilterMode.NONE,
        okhttp = false,
        webSockets = false,
        handlers = false,
        executors = false,
        coroutines = false,
        interactionOperations = false,
        logSpam = false,
        classGraph = false,
        runtimeCallGraph = false,
        classGraphDirectory = "",
        instrumentationDiagnosticsDirectory = "",
        androidComponents = true,
    )

    private data class ReceiverCalls(
        var enters: Int = 0,
        var exits: Int = 0,
        var asyncRegistrations: Int = 0,
        val finishOrder: MutableList<String> = mutableListOf(),
    )

    private companion object {
        const val ANDROID_RECEIVER = "android/content/BroadcastReceiver"
        const val ANDROID_PENDING_RESULT = "android/content/BroadcastReceiver${'$'}PendingResult"
        const val JANK_HUNTER_ANDROID_HOOKS = "io/jankhunter/runtime/JankHunterAndroidHooks"
    }
}
