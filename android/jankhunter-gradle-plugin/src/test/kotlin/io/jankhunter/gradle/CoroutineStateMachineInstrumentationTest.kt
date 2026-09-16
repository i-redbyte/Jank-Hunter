package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes

class CoroutineStateMachineInstrumentationTest {
    @Test
    fun invokeSuspendGetsSegmentHooksOnReturnAndExceptionUnwind() {
        val bytes = instrument(coroutines = true, coroutineHierarchy = true)
        val hooks = mutableListOf<String>()
        var catchAllHandlers = 0

        ClassReader(bytes).accept(object : ClassVisitor(Opcodes.ASM9) {
            override fun visitMethod(
                access: Int,
                name: String,
                descriptor: String,
                signature: String?,
                exceptions: Array<out String>?,
            ): MethodVisitor = object : MethodVisitor(Opcodes.ASM9) {
                override fun visitTryCatchBlock(
                    start: org.objectweb.asm.Label,
                    end: org.objectweb.asm.Label,
                    handler: org.objectweb.asm.Label,
                    type: String?,
                ) {
                    if (name == "invokeSuspend" && type == null) catchAllHandlers++
                }

                override fun visitMethodInsn(
                    opcodeAndSource: Int,
                    owner: String,
                    methodName: String,
                    methodDescriptor: String,
                    isInterface: Boolean,
                ) {
                    if (name == "invokeSuspend" && owner == JANK_HUNTER_HOOKS) {
                        hooks += "$methodName$methodDescriptor"
                    }
                }
            }
        }, 0)

        assertEquals(1, hooks.count { it.startsWith("enterCoroutineSegment") })
        assertEquals(2, hooks.count { it.startsWith("exitCoroutineSegment") })
        assertEquals(1, catchAllHandlers)
        assertTrue(bytes.toString(Charsets.ISO_8859_1).contains("example.Fetcher.load"))
    }

    @Test
    fun similarlyNamedOrdinaryMethodIsNotInstrumented() {
        val hooks = hookNames(instrument(coroutines = true, coroutineHierarchy = false))

        assertTrue(hooks.none { it.contains("CoroutineSegment") })
    }

    @Test
    fun disabledCoroutineTracingDoesNotInstrumentStateMachine() {
        val hooks = hookNames(instrument(coroutines = false, coroutineHierarchy = true))

        assertTrue(hooks.none { it.contains("CoroutineSegment") })
    }

    @Test
    fun generatedLocalCoroutineUsesEnclosingMethodAsOwner() {
        val bytes = instrument(
            coroutines = true,
            coroutineHierarchy = true,
            source = fixture(
                className = "example/Fetcher${'$'}execute${'$'}localResult${'$'}1",
                enclosingOwner = "example/Fetcher",
                enclosingMethod = "execute",
            ),
        )

        assertEquals(listOf("example.Fetcher.execute"), coroutineOwners(bytes))
    }

    @Test
    fun nestedCoroutineSkipsGeneratedEnclosingInvokeSuspendOwner() {
        val bytes = instrument(
            coroutines = true,
            coroutineHierarchy = true,
            source = fixture(
                className = "example/Fetcher${'$'}collect${'$'}1${'$'}1${'$'}1",
                enclosingOwner = "example/Fetcher${'$'}collect${'$'}1${'$'}1",
                enclosingMethod = "invokeSuspend",
            ),
        )

        assertEquals(listOf("example.Fetcher.collect"), coroutineOwners(bytes))
    }

    private fun instrument(
        coroutines: Boolean,
        coroutineHierarchy: Boolean,
        source: ByteArray = fixture(),
    ): ByteArray {
        val reader = ClassReader(source)
        val writer = ClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        val hierarchy = linkedSetOf(reader.className)
        if (coroutineHierarchy) hierarchy += "kotlin/coroutines/jvm/internal/BaseContinuationImpl"
        reader.accept(
            JankHunterClassVisitor(writer, reader.className, config(coroutines), classHierarchy = hierarchy),
            ClassReader.EXPAND_FRAMES,
        )
        return writer.toByteArray()
    }

    private fun coroutineOwners(bytes: ByteArray): List<String> {
        val owners = mutableListOf<String>()
        ClassReader(bytes).accept(object : ClassVisitor(Opcodes.ASM9) {
            override fun visitMethod(
                access: Int,
                name: String,
                descriptor: String,
                signature: String?,
                exceptions: Array<out String>?,
            ): MethodVisitor = object : MethodVisitor(Opcodes.ASM9) {
                private var lastString: String? = null

                override fun visitLdcInsn(value: Any?) {
                    lastString = value as? String
                }

                override fun visitMethodInsn(
                    opcodeAndSource: Int,
                    owner: String,
                    methodName: String,
                    descriptor: String,
                    isInterface: Boolean,
                ) {
                    if (owner == JANK_HUNTER_HOOKS && methodName == "enterCoroutineSegment") {
                        owners += checkNotNull(lastString)
                    }
                }
            }
        }, 0)
        return owners
    }

    private fun hookNames(bytes: ByteArray): List<String> {
        val hooks = mutableListOf<String>()
        ClassReader(bytes).accept(object : ClassVisitor(Opcodes.ASM9) {
            override fun visitMethod(
                access: Int,
                name: String,
                descriptor: String,
                signature: String?,
                exceptions: Array<out String>?,
            ): MethodVisitor = object : MethodVisitor(Opcodes.ASM9) {
                override fun visitMethodInsn(
                    opcodeAndSource: Int,
                    owner: String,
                    methodName: String,
                    descriptor: String,
                    isInterface: Boolean,
                ) {
                    if (owner == JANK_HUNTER_HOOKS) hooks += methodName
                }
            }
        }, 0)
        return hooks
    }

    private fun fixture(
        className: String = "example/Fetcher${'$'}load${'$'}1",
        enclosingOwner: String? = null,
        enclosingMethod: String? = null,
    ): ByteArray {
        val writer = ClassWriter(ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, className, null, "java/lang/Object", null)
        if (enclosingOwner != null && enclosingMethod != null) {
            writer.visitOuterClass(enclosingOwner, enclosingMethod, "()V")
        }
        writer.visitMethod(
            Opcodes.ACC_PUBLIC,
            "invokeSuspend",
            "(Ljava/lang/Object;)Ljava/lang/Object;",
            null,
            null,
        ).apply {
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 1)
            visitInsn(Opcodes.ARETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun config(coroutines: Boolean): HookConfig = HookConfig(
        methodCounters = false,
        okhttp = false,
        webSockets = false,
        handlers = false,
        executors = false,
        coroutines = coroutines,
        interactionOperations = false,
        logSpam = false,
        classGraph = false,
        runtimeCallGraph = false,
        classGraphDirectory = "",
        instrumentationDiagnosticsDirectory = "",
    )

    private companion object {
        const val JANK_HUNTER_HOOKS = "io/jankhunter/runtime/JankHunterHooks"
    }
}
