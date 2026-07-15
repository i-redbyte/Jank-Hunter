package io.jankhunter.gradle

import org.gradle.api.GradleException
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.AnnotationVisitor
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Label
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes
import org.objectweb.asm.util.CheckClassAdapter
import java.nio.file.Files

class InstrumentationSmokeTest {
    @Test
    fun instrumentsMixedAndroidSdkCallSitesIntoVerifiableBytecode() {
        val instrumented = instrument(mixedHookFixture(), okHttpHelperAvailable = true)
        val verifierDiagnostics = java.io.StringWriter()
        CheckClassAdapter.verify(ClassReader(instrumented), false, java.io.PrintWriter(verifierDiagnostics))
        assertTrue(
            "ASM verifier rejected instrumented bytecode:\n$verifierDiagnostics",
            verifierDiagnostics.toString().isBlank(),
        )
        val calls = collectCalls(instrumented)

        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunterHooks", "enterAnnotatedContext")))
        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunterHooks", "wrapHandlerRunnable")))
        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunterHooks", "onHandlerPostResult")))
        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunterHooks", "handlerWrappers")))
        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunterHooks", "clearHandlerWrappers")))
        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunterHooks", "wrapRunnable")))
        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunterHooks", "wrapCoroutineBlock")))
        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunterHooks", "wrapClickListener")))
        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunterHooks", "watchLifecycleObject")))
        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunterHooks", "recordLogSpam")))
        assertTrue(calls.contains(Call("io/jankhunter/okhttp3/JankHunterOkHttp3", "wrapEventListenerFactory")))
        assertTrue(calls.contains(Call("io/jankhunter/okhttp3/JankHunterOkHttp3", "installEventListenerFactory")))
        assertTrue(calls.contains(Call("io/jankhunter/okhttp3/JankHunterOkHttp3", "wrapWebSocketListener")))
        assertTrue(calls.contains(Call("android/os/Handler", "post")))
        assertEquals(1, countMethodCalls(instrumented, "exercise", "android/os/Handler", "post"))
        assertEquals(
            2,
            countMethodCalls(
                instrumented,
                "exercise",
                "io/jankhunter/runtime/JankHunterHooks",
                "onHandlerPostResult",
            ),
        )
        assertTrue(
            methodCalls(instrumented, "onViewRecycled")
                .none { it == Call("io/jankhunter/runtime/JankHunterHooks", "watchLifecycleObject") },
        )
        assertTrue(
            methodCalls(instrumented, "onCleared").contains(
                Call("io/jankhunter/runtime/JankHunterHooks", "watchLifecycleObject"),
            ),
        )
    }

    @Test
    fun matchedOkHttpHookWithoutRuntimeHelperFailsBeforeEmittingCrashableBytecode() {
        val error = assertThrows(GradleException::class.java) {
            instrument(mixedHookFixture(), okHttpHelperAvailable = false)
        }

        val message = error.message.orEmpty()
        assertTrue(message.contains("example.AsmSmoke#exercise"))
        assertTrue(message.contains("line 73"))
        assertTrue(message.contains("okhttp3.OkHttpClient\$Builder.eventListenerFactory"))
        assertTrue(message.contains("jankhunter-okhttp3"))
        assertTrue(message.contains("Instrumentation stopped before emitting bytecode"))
    }

    @Test
    fun missingRuntimeHelperDoesNotFailWithoutMatchedNetworkCallSite() {
        val instrumented = instrument(nonNetworkFixture(), okHttpHelperAvailable = false)
        val calls = collectCalls(instrumented)

        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunterHooks", "recordMethodCall")))
        assertTrue(calls.none { call -> call.owner == "io/jankhunter/okhttp3/JankHunterOkHttp3" })
    }

    @Test
    fun matchedWebSocketHookWithoutRuntimeHelperAlsoFails() {
        val error = assertThrows(GradleException::class.java) {
            instrument(webSocketFixture(), okHttpHelperAvailable = false)
        }

        val message = error.message.orEmpty()
        assertTrue(message.contains("newWebSocket"))
        assertTrue(message.contains("line 91"))
    }

    @Test
    fun instrumentationMarkerMakesRepeatedTransformIdempotent() {
        val once = instrument(mixedHookFixture())
        val twice = instrument(once)

        assertEquals(
            countMethodCalls(once, "exercise", "io/jankhunter/runtime/JankHunterHooks", "wrapRunnable"),
            countMethodCalls(twice, "exercise", "io/jankhunter/runtime/JankHunterHooks", "wrapRunnable"),
        )
        assertEquals(1, countClassAnnotation(twice, InstrumentationMarker.DESCRIPTOR))
    }

    @Test
    fun lifecycleHooksRequireSupportedAndroidHierarchy() {
        val instrumented = instrument(
            mixedHookFixture(),
            classHierarchy = setOf("example/AsmSmoke", "java/lang/Object"),
        )

        assertTrue(
            methodCalls(instrumented, "onViewRecycled")
                .none { it == Call("io/jankhunter/runtime/JankHunterHooks", "watchLifecycleObject") },
        )
    }

    @Test
    fun appliedCallSiteHooksWriteStableOwnerMapEntries() {
        val entries = Files.createTempDirectory("jankhunter-hook-owner-map").toFile()

        instrument(
            mixedHookFixture(),
            ownerMapEntriesDirectory = entries.absolutePath,
            methodCounters = false,
            runtimeCallGraph = false,
        )

        val text = InstrumentationArtifactFiles.readJsonlLines(entries).joinToString("\n")
        assertTrue(text.contains("\"owner\":\"example.AsmSmoke.exercise\""))
        assertTrue(text.contains("\"id\":\"stable:0x"))
    }

    @Test
    fun constructorsOnlyInstrumentCallSitesAfterSuperAndClassInitializersStayUntouched() {
        val instrumented = instrument(mixedHookFixture())
        val constructorCalls = methodCalls(instrumented, "<init>")
        val classInitializerCalls = methodCalls(instrumented, "<clinit>")

        assertTrue(constructorCalls.contains(Call("io/jankhunter/runtime/JankHunterHooks", "wrapHandlerRunnable")))
        assertEquals(1, countMethodCalls(instrumented, "<init>", "android/os/Handler", "post"))
        setOf(
            "recordMethodCall",
            "enterMethod",
            "exitMethod",
            "enterAnnotatedContext",
            "exitAnnotatedContext",
            "watchLifecycleObject",
        ).forEach { forbidden ->
            assertTrue(constructorCalls.none { it == Call("io/jankhunter/runtime/JankHunterHooks", forbidden) })
        }
        assertTrue(classInitializerCalls.none { it.owner.startsWith("io/jankhunter/") })
        assertTrue(classInitializerCalls.contains(Call("android/util/Log", "d")))
    }

    private fun instrument(
        bytes: ByteArray,
        classHierarchy: Set<String> = setOf(
            "example/AsmSmoke",
            "androidx/lifecycle/ViewModel",
            "androidx/recyclerview/widget/RecyclerView\$Adapter",
        ),
        ownerMapEntriesDirectory: String = "",
        methodCounters: Boolean = true,
        runtimeCallGraph: Boolean = true,
        okHttpHelperAvailable: Boolean = true,
    ): ByteArray {
        val reader = ClassReader(bytes)
        val writer = SafeClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        reader.accept(
            JankHunterClassVisitor(
                writer,
                "example/AsmSmoke",
                HookConfig(
                    methodCounters = methodCounters,
                    okhttp = true,
                    webSockets = true,
                    okHttpHelperAvailable = okHttpHelperAvailable,
                    handlers = true,
                    executors = true,
                    coroutines = true,
                    flowInteractions = true,
                    logSpam = true,
                    classGraph = true,
                    runtimeCallGraph = runtimeCallGraph,
                    classGraphDirectory = "",
                    instrumentationDiagnosticsDirectory = "",
                    ownerMapEntriesDirectory = ownerMapEntriesDirectory,
                    lifecycleLeaks = true,
                ),
                classHierarchy = classHierarchy,
            ),
            ClassReader.EXPAND_FRAMES,
        )
        return writer.toByteArray()
    }

    private fun mixedHookFixture(): ByteArray {
        val writer = SafeClassWriter(null, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "example/AsmSmoke", null, "java/lang/Object", null)
        writer.visitMethod(
            Opcodes.ACC_PUBLIC,
            "<init>",
            "(Landroid/os/Handler;Ljava/lang/Runnable;)V",
            null,
            null,
        ).apply {
            visitAnnotation(TRACE_DESCRIPTOR, false).stringValue("constructor")
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            visitMethodInsn(Opcodes.INVOKESPECIAL, "java/lang/Object", "<init>", "()V", false)
            visitVarInsn(Opcodes.ALOAD, 1)
            visitVarInsn(Opcodes.ALOAD, 2)
            visitMethodInsn(Opcodes.INVOKEVIRTUAL, "android/os/Handler", "post", "(Ljava/lang/Runnable;)Z", false)
            visitInsn(Opcodes.POP)
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitMethod(Opcodes.ACC_STATIC, "<clinit>", "()V", null, null).apply {
            visitCode()
            visitLdcInsn("JankHunter")
            visitLdcInsn("class init")
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
        writer.visitMethod(Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC, "exercise", MIXED_DESCRIPTOR, null, null).apply {
            visitAnnotation(TRACE_DESCRIPTOR, false).stringValue("asmSmoke")
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            visitVarInsn(Opcodes.ALOAD, 1)
            visitMethodInsn(Opcodes.INVOKEVIRTUAL, "android/os/Handler", "post", "(Ljava/lang/Runnable;)Z", false)
            visitInsn(Opcodes.POP)

            visitVarInsn(Opcodes.ALOAD, 0)
            visitVarInsn(Opcodes.ALOAD, 1)
            visitMethodInsn(Opcodes.INVOKEVIRTUAL, "android/os/Handler", "removeCallbacks", "(Ljava/lang/Runnable;)V", false)

            visitVarInsn(Opcodes.ALOAD, 0)
            visitVarInsn(Opcodes.ALOAD, 1)
            visitMethodInsn(Opcodes.INVOKEVIRTUAL, "android/os/Handler", "hasCallbacks", "(Ljava/lang/Runnable;)Z", false)
            visitInsn(Opcodes.POP)

            visitVarInsn(Opcodes.ALOAD, 0)
            visitInsn(Opcodes.ACONST_NULL)
            visitMethodInsn(
                Opcodes.INVOKEVIRTUAL,
                "android/os/Handler",
                "removeCallbacksAndMessages",
                "(Ljava/lang/Object;)V",
                false,
            )

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
            val okHttpLine = Label()
            visitLabel(okHttpLine)
            visitLineNumber(73, okHttpLine)
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

    private fun nonNetworkFixture(): ByteArray {
        val writer = SafeClassWriter(null, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "example/AsmSmoke", null, "java/lang/Object", null)
        writer.visitMethod(Opcodes.ACC_PUBLIC, "work", "()V", null, null).apply {
            visitCode()
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun webSocketFixture(): ByteArray {
        val writer = SafeClassWriter(null, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "example/AsmSmoke", null, "java/lang/Object", null)
        writer.visitMethod(
            Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC,
            "openSocket",
            "(Lokhttp3/OkHttpClient;Lokhttp3/Request;Lokhttp3/WebSocketListener;)V",
            null,
            null,
        ).apply {
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            visitVarInsn(Opcodes.ALOAD, 1)
            visitVarInsn(Opcodes.ALOAD, 2)
            val line = Label()
            visitLabel(line)
            visitLineNumber(91, line)
            visitMethodInsn(
                Opcodes.INVOKEVIRTUAL,
                "okhttp3/OkHttpClient",
                "newWebSocket",
                "(Lokhttp3/Request;Lokhttp3/WebSocketListener;)Lokhttp3/WebSocket;",
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

    private fun countMethodCalls(bytes: ByteArray, targetMethod: String, owner: String, methodName: String): Int {
        var count = 0
        ClassReader(bytes).accept(
            object : ClassVisitor(Opcodes.ASM9) {
                override fun visitMethod(
                    access: Int,
                    name: String,
                    descriptor: String,
                    signature: String?,
                    exceptions: Array<out String>?,
                ): MethodVisitor? {
                    if (name != targetMethod) return null
                    return object : MethodVisitor(Opcodes.ASM9) {
                        override fun visitMethodInsn(
                            opcodeAndSource: Int,
                            callOwner: String,
                            callName: String,
                            descriptor: String,
                            isInterface: Boolean,
                        ) {
                            if (callOwner == owner && callName == methodName) count += 1
                        }
                    }
                }
            },
            0,
        )
        return count
    }

    private fun countClassAnnotation(bytes: ByteArray, descriptor: String): Int {
        var count = 0
        ClassReader(bytes).accept(
            object : ClassVisitor(Opcodes.ASM9) {
                override fun visitAnnotation(annotationDescriptor: String, visible: Boolean): AnnotationVisitor? {
                    if (annotationDescriptor == descriptor) count += 1
                    return null
                }
            },
            0,
        )
        return count
    }

    private class SafeClassWriter(
        reader: ClassReader?,
        flags: Int,
    ) : ClassWriter(reader, flags) {
        override fun getCommonSuperClass(type1: String, type2: String): String = "java/lang/Object"
    }

    private companion object {
        private const val TRACE_DESCRIPTOR = "Lio/jankhunter/annotations/JankHunterTrace;"
        private const val MIXED_DESCRIPTOR =
            "(Landroid/os/Handler;Ljava/lang/Runnable;Ljava/util/concurrent/Executor;" +
                "Lkotlinx/coroutines/CoroutineScope;Lkotlin/coroutines/CoroutineContext;" +
                "Lkotlinx/coroutines/CoroutineStart;Lkotlin/jvm/functions/Function2;" +
                "Landroid/view/View;Landroid/view/View\$OnClickListener;" +
                "Lokhttp3/OkHttpClient\$Builder;Lokhttp3/OkHttpClient;Lokhttp3/Request;" +
                "Lokhttp3/WebSocketListener;Lokhttp3/EventListener\$Factory;)V"
    }
}
