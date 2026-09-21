package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Handle
import org.objectweb.asm.Opcodes
import org.objectweb.asm.Type
import org.objectweb.asm.tree.ClassNode

class LambdaCaptureAnalyzerTest {
    @Test
    fun bestEffortAnalysisDoesNotSwallowCancellationOrFatalErrors() {
        assertThrows(InterruptedException::class.java) {
            bestEffortAsmAnalysis<Unit> { throw InterruptedException("cancel build") }
        }
        assertThrows(AssertionError::class.java) {
            bestEffortAsmAnalysis<Unit> { throw AssertionError("fatal") }
        }
        assertEquals(null, bestEffortAsmAnalysis<Unit> { throw IllegalArgumentException("invalid bytecode") })
    }

    @Test
    fun invokedynamicCaptureTracksComposeSinkAndWeakForceUnwrap() {
        val node = readClass(invokedynamicFixture())

        val capture = LambdaCaptureAnalyzer.analyze(node).single()

        assertEquals("invokedynamic", capture.representation)
        assertEquals("example.Screen.render(Landroid/app/Activity;Ljava/lang/ref/WeakReference;Landroidx/compose/runtime/Composer;)V", capture.owner)
        assertEquals("example.Screen.render\$lambda\$0", capture.implementation)
        assertEquals("kotlin.jvm.functions.Function0", capture.functionalInterface)
        assertEquals(listOf("compose.remember"), capture.sinks)
        assertEquals("force_unwrap", capture.weakDereference)
        assertEquals(
            listOf(
                LambdaCapturedValue("android.app.Activity", "capture", "strong"),
                LambdaCapturedValue("java.lang.ref.WeakReference", "capture", "weak"),
            ),
            capture.values,
        )
        assertFalse(capture.desugared)
    }

    @Test
    fun weakGetDoesNotInheritForceUnwrapFromUnrelatedValue() {
        val capture = LambdaCaptureAnalyzer.analyze(
            readClass(invokedynamicFixture(forceUnwrapWeak = false)),
        ).single()

        assertEquals(null, capture.weakDereference)
    }

    @Test
    fun composeCacheInitializerIsNotReportedAsRetainedLambda() {
        val capture = LambdaCaptureAnalyzer.analyze(
            readClass(invokedynamicFixture(composerCacheInitializer = true)),
        ).single()

        assertTrue(capture.sinks.isEmpty())
    }

    @Test
    fun flowCaptureIsLinkedToGlobalScopeLaunchInWithinSameMethod() {
        val capture = LambdaCaptureAnalyzer.analyze(readClass(flowFixture())).single()

        assertEquals(listOf("flow.global_scope", "flow.operator"), capture.sinks)
    }

    @Test
    fun coroutineCaptureIsLinkedToGlobalScopeBuilder() {
        val capture = LambdaCaptureAnalyzer.analyze(readClass(globalCoroutineFixture())).single()

        assertEquals(listOf("coroutine.builder", "coroutine.global_scope"), capture.sinks)
    }

    @Test
    fun sinkAssociationAccountsForWideJvmArguments() {
        val capture = LambdaCaptureAnalyzer.analyze(readClass(wideSinkFixture())).single()

        assertEquals(listOf("listener.registration"), capture.sinks)
    }

    @Test
    fun classBasedCoroutineSeparatesConstructorCaptureFromSpillSlot() {
        val capture = LambdaCaptureAnalyzer.analyze(readClass(coroutineFixture())).single()

        assertEquals("coroutine", capture.representation)
        assertEquals("example.Screen.load", capture.owner)
        assertEquals(
            listOf(
                LambdaCapturedValue("android.app.Activity", "capture:this\$0", "strong"),
                LambdaCapturedValue("android.view.View", "spill:L\$0", "strong"),
            ),
            capture.values,
        )
        assertFalse(capture.desugared)
    }

    @Test
    fun classBasedDesugaredLambdaIsMarkedAndMethodSuppressionIsPreserved() {
        val node = readClass(desugaredFixture())

        val capture = LambdaCaptureAnalyzer.analyze(node).single()

        assertEquals("class", capture.representation)
        assertTrue(capture.desugared)
        assertTrue(capture.suppressed)
        assertEquals("owned by process singleton", capture.suppressionReason)
    }

    @Test
    fun blankSuppressionReasonIsNormalizedForArtifactContract() {
        val node = readClass(desugaredFixture(suppressionReason = "  "))

        val capture = LambdaCaptureAnalyzer.analyze(node).single()

        assertTrue(capture.suppressed)
        assertEquals("reason not provided", capture.suppressionReason)
    }

    private fun readClass(bytes: ByteArray): ClassNode {
        return ClassNode(Opcodes.ASM9).also { ClassReader(bytes).accept(it, 0) }
    }

    private fun invokedynamicFixture(
        forceUnwrapWeak: Boolean = true,
        composerCacheInitializer: Boolean = false,
    ): ByteArray {
        val writer = baseClass("example/Screen")
        writer.visitMethod(
            Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC,
            "render",
            "(Landroid/app/Activity;Ljava/lang/ref/WeakReference;Landroidx/compose/runtime/Composer;)V",
            null,
            null,
        ).apply {
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 2)
            if (composerCacheInitializer) visitInsn(Opcodes.ICONST_0)
            visitVarInsn(Opcodes.ALOAD, 0)
            visitVarInsn(Opcodes.ALOAD, 1)
            visitInvokeDynamicInsn(
                "invoke",
                "(Landroid/app/Activity;Ljava/lang/ref/WeakReference;)Lkotlin/jvm/functions/Function0;",
                LAMBDA_METAFACTORY,
                Type.getMethodType("()Ljava/lang/Object;"),
                Handle(
                    Opcodes.H_INVOKESTATIC,
                    "example/Screen",
                    "render\$lambda\$0",
                    "(Landroid/app/Activity;Ljava/lang/ref/WeakReference;)Ljava/lang/Object;",
                    false,
                ),
                Type.getMethodType("()Ljava/lang/Object;"),
            )
            if (composerCacheInitializer) {
                visitMethodInsn(
                    Opcodes.INVOKEINTERFACE,
                    "androidx/compose/runtime/Composer",
                    "cache",
                    "(ZLkotlin/jvm/functions/Function0;)Ljava/lang/Object;",
                    true,
                )
                visitInsn(Opcodes.POP)
            } else {
                visitMethodInsn(
                    Opcodes.INVOKEINTERFACE,
                    "androidx/compose/runtime/Composer",
                    "updateRememberedValue",
                    "(Ljava/lang/Object;)V",
                    true,
                )
            }
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitMethod(
            Opcodes.ACC_PRIVATE or Opcodes.ACC_STATIC,
            "render\$lambda\$0",
            "(Landroid/app/Activity;Ljava/lang/ref/WeakReference;)Ljava/lang/Object;",
            null,
            null,
        ).apply {
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 1)
            visitMethodInsn(Opcodes.INVOKEVIRTUAL, "java/lang/ref/WeakReference", "get", "()Ljava/lang/Object;", false)
            if (!forceUnwrapWeak) {
                visitInsn(Opcodes.POP)
                visitVarInsn(Opcodes.ALOAD, 0)
            }
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                "kotlin/jvm/internal/Intrinsics",
                "checkNotNull",
                "(Ljava/lang/Object;)V",
                false,
            )
            visitVarInsn(Opcodes.ALOAD, 0)
            visitInsn(Opcodes.ARETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun flowFixture(): ByteArray {
        val writer = baseClass("example/Feed")
        writer.visitMethod(
            Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC,
            "observe",
            "(Landroid/app/Activity;Lkotlinx/coroutines/flow/Flow;)V",
            null,
            null,
        ).apply {
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 1)
            visitVarInsn(Opcodes.ALOAD, 0)
            visitInvokeDynamicInsn(
                "invoke",
                "(Landroid/app/Activity;)Lkotlin/jvm/functions/Function2;",
                LAMBDA_METAFACTORY,
                Type.getMethodType("(Ljava/lang/Object;Ljava/lang/Object;)Ljava/lang/Object;"),
                Handle(
                    Opcodes.H_INVOKESTATIC,
                    "example/Feed",
                    "observe\$lambda\$0",
                    "(Landroid/app/Activity;Ljava/lang/Object;Ljava/lang/Object;)Ljava/lang/Object;",
                    false,
                ),
                Type.getMethodType("(Ljava/lang/Object;Ljava/lang/Object;)Ljava/lang/Object;"),
            )
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                "kotlinx/coroutines/flow/FlowKt",
                "onEach",
                "(Lkotlinx/coroutines/flow/Flow;Lkotlin/jvm/functions/Function2;)Lkotlinx/coroutines/flow/Flow;",
                false,
            )
            visitFieldInsn(Opcodes.GETSTATIC, "kotlinx/coroutines/GlobalScope", "INSTANCE", "Lkotlinx/coroutines/GlobalScope;")
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                "kotlinx/coroutines/flow/FlowKt",
                "launchIn",
                "(Lkotlinx/coroutines/flow/Flow;Lkotlinx/coroutines/CoroutineScope;)Lkotlinx/coroutines/Job;",
                false,
            )
            visitInsn(Opcodes.POP)
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitMethod(
            Opcodes.ACC_PRIVATE or Opcodes.ACC_STATIC,
            "observe\$lambda\$0",
            "(Landroid/app/Activity;Ljava/lang/Object;Ljava/lang/Object;)Ljava/lang/Object;",
            null,
            null,
        ).apply {
            visitCode()
            visitInsn(Opcodes.ACONST_NULL)
            visitInsn(Opcodes.ARETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun wideSinkFixture(): ByteArray {
        val writer = baseClass("example/WideSink")
        writer.visitMethod(
            Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC,
            "register",
            "(Landroid/app/Activity;)V",
            null,
            null,
        ).apply {
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            visitInvokeDynamicInsn(
                "invoke",
                "(Landroid/app/Activity;)Lkotlin/jvm/functions/Function0;",
                LAMBDA_METAFACTORY,
                Type.getMethodType("()Ljava/lang/Object;"),
                Handle(
                    Opcodes.H_INVOKESTATIC,
                    "example/WideSink",
                    "register\$lambda\$0",
                    "(Landroid/app/Activity;)Ljava/lang/Object;",
                    false,
                ),
                Type.getMethodType("()Ljava/lang/Object;"),
            )
            visitInsn(Opcodes.LCONST_0)
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                "example/ListenerRegistry",
                "setOnEventListener",
                "(Lkotlin/jvm/functions/Function0;J)V",
                false,
            )
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitMethod(
            Opcodes.ACC_PRIVATE or Opcodes.ACC_STATIC,
            "register\$lambda\$0",
            "(Landroid/app/Activity;)Ljava/lang/Object;",
            null,
            null,
        ).apply {
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            visitInsn(Opcodes.ARETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun coroutineFixture(): ByteArray {
        val writer = ClassWriter(0)
        writer.visit(
            Opcodes.V17,
            Opcodes.ACC_FINAL or Opcodes.ACC_SYNTHETIC,
            "example/Screen\$load\$1",
            null,
            "kotlin/coroutines/jvm/internal/SuspendLambda",
            arrayOf("kotlin/jvm/functions/Function2"),
        )
        writer.visitOuterClass("example/Screen", "load", "()V")
        writer.visitField(Opcodes.ACC_FINAL or Opcodes.ACC_SYNTHETIC, "this\$0", "Landroid/app/Activity;", null, null).visitEnd()
        writer.visitField(Opcodes.ACC_SYNTHETIC, "L\$0", "Landroid/view/View;", null, null).visitEnd()
        writer.visitField(Opcodes.ACC_SYNTHETIC, "result", "Ljava/lang/Object;", null, null).visitEnd()
        writer.visitField(Opcodes.ACC_SYNTHETIC, "label", "I", null, null).visitEnd()
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun globalCoroutineFixture(): ByteArray {
        val writer = baseClass("example/GlobalWork")
        writer.visitMethod(
            Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC,
            "start",
            "(Landroid/app/Activity;)V",
            null,
            null,
        ).apply {
            visitCode()
            visitFieldInsn(
                Opcodes.GETSTATIC,
                "kotlinx/coroutines/GlobalScope",
                "INSTANCE",
                "Lkotlinx/coroutines/GlobalScope;",
            )
            visitVarInsn(Opcodes.ALOAD, 0)
            visitInvokeDynamicInsn(
                "invoke",
                "(Landroid/app/Activity;)Lkotlin/jvm/functions/Function2;",
                LAMBDA_METAFACTORY,
                Type.getMethodType("(Ljava/lang/Object;Ljava/lang/Object;)Ljava/lang/Object;"),
                Handle(
                    Opcodes.H_INVOKESTATIC,
                    "example/GlobalWork",
                    "start\$lambda\$0",
                    "(Landroid/app/Activity;Ljava/lang/Object;Ljava/lang/Object;)Ljava/lang/Object;",
                    false,
                ),
                Type.getMethodType("(Ljava/lang/Object;Ljava/lang/Object;)Ljava/lang/Object;"),
            )
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                "kotlinx/coroutines/BuildersKt",
                "launch",
                "(Lkotlinx/coroutines/CoroutineScope;Lkotlin/jvm/functions/Function2;)Lkotlinx/coroutines/Job;",
                false,
            )
            visitInsn(Opcodes.POP)
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitMethod(
            Opcodes.ACC_PRIVATE or Opcodes.ACC_STATIC,
            "start\$lambda\$0",
            "(Landroid/app/Activity;Ljava/lang/Object;Ljava/lang/Object;)Ljava/lang/Object;",
            null,
            null,
        ).apply {
            visitCode()
            visitInsn(Opcodes.ACONST_NULL)
            visitInsn(Opcodes.ARETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun desugaredFixture(suppressionReason: String = "owned by process singleton"): ByteArray {
        val writer = ClassWriter(0)
        writer.visit(
            Opcodes.V17,
            Opcodes.ACC_FINAL or Opcodes.ACC_SYNTHETIC,
            "example/Screen\$\$ExternalSyntheticLambda0",
            null,
            "java/lang/Object",
            arrayOf("java/lang/Runnable"),
        )
        writer.visitOuterClass("example/Screen", "bind", "()V")
        writer.visitAnnotation(SUPPRESS_DESCRIPTOR, false).apply {
            visit("reason", suppressionReason)
            visitEnd()
        }
        writer.visitField(Opcodes.ACC_FINAL or Opcodes.ACC_SYNTHETIC, "f\$0", "Landroid/app/Activity;", null, null).visitEnd()
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun baseClass(name: String): ClassWriter {
        return ClassWriter(ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS).apply {
            visit(Opcodes.V17, Opcodes.ACC_PUBLIC, name, null, "java/lang/Object", null)
        }
    }

    private companion object {
        const val SUPPRESS_DESCRIPTOR = "Lio/jankhunter/annotations/JankHunterSuppress;"
        val LAMBDA_METAFACTORY = Handle(
            Opcodes.H_INVOKESTATIC,
            "java/lang/invoke/LambdaMetafactory",
            "metafactory",
            "(Ljava/lang/invoke/MethodHandles\u0024Lookup;Ljava/lang/String;Ljava/lang/invoke/MethodType;" +
                "Ljava/lang/invoke/MethodType;Ljava/lang/invoke/MethodHandle;Ljava/lang/invoke/MethodType;)Ljava/lang/invoke/CallSite;",
            false,
        )
    }
}
