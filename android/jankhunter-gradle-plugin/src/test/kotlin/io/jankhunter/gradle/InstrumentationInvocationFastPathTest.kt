package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes
import org.objectweb.asm.Type

class InstrumentationInvocationFastPathTest {
    @Test
    fun irrelevantInvocationDoesNotResolveOwnerHierarchy() {
        var hierarchyResolutions = 0

        val output = instrument(
            invocationOwner = "java/lang/String",
            invocationName = "length",
            invocationDescriptor = "()I",
            resolveOwnerHierarchy = { owner ->
                hierarchyResolutions++
                setOf(owner)
            },
        )

        assertEquals(0, hierarchyResolutions)
        assertEquals(0, handlerPostObservationCount(output))
    }

    @Test
    fun exactOwnerMatchDoesNotResolveOwnerHierarchy() {
        var hierarchyResolutions = 0

        val output = instrument(
            invocationOwner = "android/os/Handler",
            invocationName = "post",
            invocationDescriptor = "(Ljava/lang/Runnable;)Z",
            resolveOwnerHierarchy = { owner ->
                hierarchyResolutions++
                setOf(owner)
            },
        )

        assertEquals(0, hierarchyResolutions)
        assertEquals(1, handlerPostObservationCount(output))
    }

    @Test
    fun inheritedOwnerCandidateResolvesHierarchyOnlyOnce() {
        var hierarchyResolutions = 0

        val output = instrument(
            invocationOwner = "example/HandlerSubclass",
            invocationName = "post",
            invocationDescriptor = "(Ljava/lang/Runnable;)Z",
            resolveOwnerHierarchy = { owner ->
                hierarchyResolutions++
                setOf(owner, "android/os/Handler")
            },
        )

        assertEquals(1, hierarchyResolutions)
        assertEquals(1, handlerPostObservationCount(output))
    }

    @Test
    fun exactOwnerDiagnosticDoesNotResolveHierarchyWhenDescriptorBelongsToAnotherModule() {
        var hierarchyResolutions = 0

        instrument(
            invocationOwner = "android/database/sqlite/SQLiteStatement",
            invocationName = "execute",
            invocationDescriptor = "(Ljava/lang/Runnable;)V",
            resolveOwnerHierarchy = { owner ->
                hierarchyResolutions++
                setOf(owner)
            },
        )

        assertEquals(0, hierarchyResolutions)
    }

    private fun instrument(
        invocationOwner: String,
        invocationName: String,
        invocationDescriptor: String,
        resolveOwnerHierarchy: (String) -> Set<String>,
    ): ByteArray {
        val input = ClassWriter(ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS).apply {
            visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "example/FastPathFixture", null, "java/lang/Object", null)
            visitMethod(
                Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC,
                "invoke",
                "(L$invocationOwner;Ljava/lang/Runnable;)V",
                null,
                null,
            ).apply {
                visitCode()
                visitVarInsn(Opcodes.ALOAD, 0)
                if (invocationDescriptor != "()I") visitVarInsn(Opcodes.ALOAD, 1)
                visitMethodInsn(
                    Opcodes.INVOKEVIRTUAL,
                    invocationOwner,
                    invocationName,
                    invocationDescriptor,
                    false,
                )
                if (Type.getReturnType(invocationDescriptor) != Type.VOID_TYPE) visitInsn(Opcodes.POP)
                visitInsn(Opcodes.RETURN)
                visitMaxs(0, 0)
                visitEnd()
            }
            visitEnd()
        }.toByteArray()
        val reader = ClassReader(input)
        val output = ClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        reader.accept(
            JankHunterClassVisitor(
                output,
                reader.className,
                config(),
                resolveOwnerHierarchy = resolveOwnerHierarchy,
            ),
            ClassReader.EXPAND_FRAMES,
        )
        return output.toByteArray()
    }

    private fun handlerPostObservationCount(bytes: ByteArray): Int {
        var count = 0
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
                            name: String,
                            descriptor: String,
                            isInterface: Boolean,
                        ) {
                            if (owner == JANK_HUNTER_HOOKS && name == "onHandlerPostResult") count++
                        }
                    }
                }
            },
            0,
        )
        return count
    }

    private fun config(): HookConfig = HookConfig(
        methodCounters = false,
        methodFilterMode = JankHunterMethodFilterMode.NONE,
        okhttp = false,
        webSockets = false,
        handlers = true,
        executors = false,
        coroutines = false,
        interactionOperations = false,
        logSpam = false,
        classGraph = false,
        runtimeCallGraph = false,
        databaseTracing = false,
        classGraphDirectory = "",
        instrumentationDiagnosticsDirectory = "",
    )

    private companion object {
        const val JANK_HUNTER_HOOKS = "io/jankhunter/runtime/JankHunterHooks"
    }
}
