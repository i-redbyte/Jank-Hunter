package io.jankhunter.gradle

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.AnnotationVisitor
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes
import org.objectweb.asm.util.CheckClassAdapter

class InstrumentationSmokeTest {
    @Test
    fun instrumentsMixedAndroidSdkCallSitesIntoVerifiableBytecode() {
        val reader = ClassReader(mixedHookFixture())
        val writer = SafeClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)

        reader.accept(
            JankHunterClassVisitor(
                writer,
                "example/AsmSmoke",
                HookConfig(
                    methodCounters = true,
                    okhttp = true,
                    webSockets = true,
                    handlers = true,
                    executors = true,
                    coroutines = true,
                    flowInteractions = true,
                    logSpam = true,
                    classGraph = true,
                    runtimeCallGraph = true,
                    classGraphDirectory = "",
                    instrumentationDiagnosticsDirectory = "",
                    ownerMapEntriesDirectory = "",
                    lifecycleLeaks = true,
                ),
            ),
            ClassReader.EXPAND_FRAMES,
        )

        val instrumented = writer.toByteArray()
        CheckClassAdapter.verify(ClassReader(instrumented), false, java.io.PrintWriter(java.io.StringWriter()))
        val calls = collectCalls(instrumented)

        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunter", "enterAnnotatedContext")))
        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunter", "postHandlerRunnable")))
        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunter", "wrapRunnable")))
        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunter", "wrapCoroutineBlock")))
        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunter", "wrapClickListener")))
        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunter", "watchLifecycleObject")))
        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunter", "recordLogSpam")))
        assertTrue(calls.contains(Call("io/jankhunter/okhttp3/JankHunterOkHttp3", "wrapEventListenerFactory")))
        assertTrue(calls.contains(Call("io/jankhunter/okhttp3/JankHunterOkHttp3", "installEventListenerFactory")))
        assertTrue(calls.contains(Call("io/jankhunter/okhttp3/JankHunterOkHttp3", "wrapWebSocketListener")))
        assertFalse(calls.contains(Call("android/os/Handler", "post")))
        assertTrue(
            methodCalls(instrumented, "onViewRecycled").contains(
                Call("io/jankhunter/runtime/JankHunter", "watchLifecycleObject"),
            ),
        )
    }

    private fun mixedHookFixture(): ByteArray {
        val writer = SafeClassWriter(null, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "example/AsmSmoke", null, "java/lang/Object", null)
        writer.visitMethod(Opcodes.ACC_PUBLIC, "<init>", "()V", null, null).apply {
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            visitMethodInsn(Opcodes.INVOKESPECIAL, "java/lang/Object", "<init>", "()V", false)
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitMethod(Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC, "exercise", MIXED_DESCRIPTOR, null, null).apply {
            visitAnnotation(TRACE_DESCRIPTOR, false).stringValue("asmSmoke")
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            visitVarInsn(Opcodes.ALOAD, 1)
            visitMethodInsn(Opcodes.INVOKEVIRTUAL, "android/os/Handler", "post", "(Ljava/lang/Runnable;)Z", false)
            visitInsn(Opcodes.POP)

            visitVarInsn(Opcodes.ALOAD, 2)
            visitVarInsn(Opcodes.ALOAD, 1)
            visitMethodInsn(Opcodes.INVOKEINTERFACE, "java/util/concurrent/Executor", "execute", "(Ljava/lang/Runnable;)V", true)

            visitVarInsn(Opcodes.ALOAD, 3)
            visitVarInsn(Opcodes.ALOAD, 4)
            visitVarInsn(Opcodes.ALOAD, 5)
            visitVarInsn(Opcodes.ALOAD, 6)
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                "kotlinx/coroutines/BuildersKt",
                "launch",
                "(Lkotlinx/coroutines/CoroutineScope;Lkotlin/coroutines/CoroutineContext;" +
                    "Lkotlinx/coroutines/CoroutineStart;Lkotlin/jvm/functions/Function2;)Lkotlinx/coroutines/Job;",
                false,
            )
            visitInsn(Opcodes.POP)

            visitVarInsn(Opcodes.ALOAD, 7)
            visitVarInsn(Opcodes.ALOAD, 8)
            visitMethodInsn(
                Opcodes.INVOKEVIRTUAL,
                "android/view/View",
                "setOnClickListener",
                "(Landroid/view/View\$OnClickListener;)V",
                false,
            )

            visitVarInsn(Opcodes.ALOAD, 9)
            visitVarInsn(Opcodes.ALOAD, 13)
            visitMethodInsn(
                Opcodes.INVOKEVIRTUAL,
                "okhttp3/OkHttpClient\$Builder",
                "eventListenerFactory",
                "(Lokhttp3/EventListener\$Factory;)Lokhttp3/OkHttpClient\$Builder;",
                false,
            )
            visitInsn(Opcodes.POP)

            visitVarInsn(Opcodes.ALOAD, 9)
            visitMethodInsn(
                Opcodes.INVOKEVIRTUAL,
                "okhttp3/OkHttpClient\$Builder",
                "build",
                "()Lokhttp3/OkHttpClient;",
                false,
            )
            visitInsn(Opcodes.POP)

            visitVarInsn(Opcodes.ALOAD, 10)
            visitVarInsn(Opcodes.ALOAD, 11)
            visitVarInsn(Opcodes.ALOAD, 12)
            visitMethodInsn(
                Opcodes.INVOKEVIRTUAL,
                "okhttp3/OkHttpClient",
                "newWebSocket",
                "(Lokhttp3/Request;Lokhttp3/WebSocketListener;)Lokhttp3/WebSocket;",
                false,
            )
            visitInsn(Opcodes.POP)

            visitLdcInsn("JankHunter")
            visitLdcInsn("asm smoke")
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
        writer.visitMethod(Opcodes.ACC_PROTECTED, "onCleared", "()V", null, null).apply {
            visitCode()
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitMethod(Opcodes.ACC_PUBLIC, "onViewRecycled", "(Ljava/lang/Object;)V", null, null).apply {
            visitCode()
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun collectCalls(bytes: ByteArray): Set<Call> {
        val calls = linkedSetOf<Call>()
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
                        override fun visitMethodInsn(
                            opcodeAndSource: Int,
                            owner: String,
                            methodName: String,
                            descriptor: String,
                            isInterface: Boolean,
                        ) {
                            calls += Call(owner, methodName)
                        }
                    }
                }
            },
            0,
        )
        return calls
    }

    private fun AnnotationVisitor.stringValue(value: String) {
        visit("value", value)
        visitEnd()
    }

    private data class Call(val owner: String, val name: String)

    private fun methodCalls(bytes: ByteArray, targetMethod: String): Set<Call> {
        val calls = linkedSetOf<Call>()
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
                        override fun visitMethodInsn(
                            opcodeAndSource: Int,
                            owner: String,
                            methodName: String,
                            descriptor: String,
                            isInterface: Boolean,
                        ) {
                            if (name == targetMethod) {
                                calls += Call(owner, methodName)
                            }
                        }
                    }
                }
            },
            0,
        )
        return calls
    }

    private class SafeClassWriter(
        reader: ClassReader?,
        flags: Int,
    ) : ClassWriter(reader, flags) {
        override fun getCommonSuperClass(type1: String, type2: String): String = "java/lang/Object"
    }

    private companion object {
        private const val TRACE_DESCRIPTOR = "Lio/jankhunter/annotations/JankTrace;"
        private const val MIXED_DESCRIPTOR =
            "(Landroid/os/Handler;Ljava/lang/Runnable;Ljava/util/concurrent/Executor;" +
                "Lkotlinx/coroutines/CoroutineScope;Lkotlin/coroutines/CoroutineContext;" +
                "Lkotlinx/coroutines/CoroutineStart;Lkotlin/jvm/functions/Function2;" +
                "Landroid/view/View;Landroid/view/View\$OnClickListener;" +
                "Lokhttp3/OkHttpClient\$Builder;Lokhttp3/OkHttpClient;Lokhttp3/Request;" +
                "Lokhttp3/WebSocketListener;Lokhttp3/EventListener\$Factory;)V"
    }
}
