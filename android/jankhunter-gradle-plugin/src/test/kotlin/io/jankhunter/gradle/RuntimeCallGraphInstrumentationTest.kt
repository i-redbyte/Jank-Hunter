package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes
import java.nio.file.Files

class RuntimeCallGraphInstrumentationTest {
    @Test
    fun runtimeCallGraphAddsCatchAllExitForExceptionUnwind() {
        val instrumented = instrumentRuntimeCallGraph(throwingFixture())
        val stats = collectMethodStats(instrumented)

        assertEquals(1, stats.parentCatchAllHandlers)
        assertEquals(2, stats.parentExitCalls)
        assertEquals(1, stats.childCatchAllHandlers)
        assertEquals(1, stats.childExitCalls)
    }

    @Test
    fun runtimeCallGraphWritesOwnerMapEntries() {
        val ownerMapEntries = Files.createTempDirectory("jankhunter-owner-map").toFile()

        instrumentRuntimeCallGraph(throwingFixture(), ownerMapEntries.absolutePath)

        val text = InstrumentationArtifactFiles.readJsonlLines(ownerMapEntries).joinToString("\n")
        assertTrue(text.contains("\"kind\":\"entry\""))
        assertTrue(text.contains("\"id\":\"stable:0x"))
        assertTrue(text.contains("\"owner\":\"example.Throwing.parent\""))
        assertTrue(text.contains("\"owner\":\"example.Throwing.child\""))
    }

    @Test
    fun embeddedSymbolsAreTheDefaultInjectedAbi() {
        val stats = collectMethodStats(instrumentRuntimeCallGraph(throwingFixture()))

        assertTrue(stats.enterDescriptors.contains("(JLjava/lang/String;)J"))
        assertTrue(!stats.enterDescriptors.contains("(J)J"))
    }

    @Test
    fun externalSymbolsKeepTheCompactInjectedAbi() {
        val stats = collectMethodStats(instrumentRuntimeCallGraph(throwingFixture(), embeddedSymbols = false))

        assertTrue(stats.enterDescriptors.contains("(J)J"))
        assertTrue(!stats.enterDescriptors.contains("(JLjava/lang/String;)J"))
    }

    @Test
    fun javaEnumScaffoldingIsExcludedWithoutDroppingApplicationMethods() {
        val instrumented = instrumentRuntimeCallGraph(javaEnumFixture())

        assertEquals(setOf("calculate"), collectInstrumentedMethods(instrumented))
    }

    private fun instrumentRuntimeCallGraph(
        bytes: ByteArray,
        ownerMapEntriesDirectory: String = "",
        embeddedSymbols: Boolean = true,
    ): ByteArray {
        val reader = ClassReader(bytes)
        val writer = ClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        reader.accept(
            JankHunterClassVisitor(
                writer,
                reader.className,
                HookConfig(
                    embeddedSymbols = embeddedSymbols,
                    methodCounters = false,
                    okhttp = false,
                    webSockets = false,
                    handlers = false,
                    executors = false,
                    coroutines = false,
                    flowInteractions = false,
                    logSpam = false,
                    classGraph = false,
                    runtimeCallGraph = true,
                    classGraphDirectory = "",
                    instrumentationDiagnosticsDirectory = "",
                    ownerMapEntriesDirectory = ownerMapEntriesDirectory,
                ),
            ),
            ClassReader.EXPAND_FRAMES,
        )
        return writer.toByteArray()
    }

    private fun collectInstrumentedMethods(bytes: ByteArray): Set<String> {
        val methods = linkedSetOf<String>()
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
                            if (owner == JANK_HUNTER_HOOKS && methodName == "enterMethod") methods += name
                        }
                    }
                }
            },
            0,
        )
        return methods
    }

    private fun collectMethodStats(bytes: ByteArray): MethodStats {
        val stats = MethodStats()
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
                        override fun visitTryCatchBlock(
                            start: org.objectweb.asm.Label,
                            end: org.objectweb.asm.Label,
                            handler: org.objectweb.asm.Label,
                            type: String?,
                        ) {
                            if (type != null) return
                            when (name) {
                                "parent" -> stats.parentCatchAllHandlers++
                                "child" -> stats.childCatchAllHandlers++
                            }
                        }

                        override fun visitMethodInsn(
                            opcodeAndSource: Int,
                            owner: String,
                            methodName: String,
                            descriptor: String,
                            isInterface: Boolean,
                        ) {
                            if (owner == "io/jankhunter/runtime/JankHunterHooks" && methodName == "enterMethod") {
                                stats.enterDescriptors += descriptor
                            }
                            if (owner != "io/jankhunter/runtime/JankHunterHooks" || methodName != "exitMethod") return
                            when (name) {
                                "parent" -> stats.parentExitCalls++
                                "child" -> stats.childExitCalls++
                            }
                        }
                    }
                }
            },
            0,
        )
        return stats
    }

    private fun throwingFixture(): ByteArray {
        val writer = ClassWriter(ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "example/Throwing", null, "java/lang/Object", null)

        writer.visitMethod(Opcodes.ACC_PUBLIC, "parent", "()V", null, null).apply {
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            visitMethodInsn(Opcodes.INVOKEVIRTUAL, "example/Throwing", "child", "()V", false)
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }

        writer.visitMethod(Opcodes.ACC_PUBLIC, "child", "()V", null, null).apply {
            visitCode()
            visitTypeInsn(Opcodes.NEW, "java/lang/RuntimeException")
            visitInsn(Opcodes.DUP)
            visitMethodInsn(Opcodes.INVOKESPECIAL, "java/lang/RuntimeException", "<init>", "()V", false)
            visitInsn(Opcodes.ATHROW)
            visitMaxs(0, 0)
            visitEnd()
        }

        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun javaEnumFixture(): ByteArray {
        val className = "example/State"
        val writer = ClassWriter(ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(
            Opcodes.V17,
            Opcodes.ACC_PUBLIC or Opcodes.ACC_FINAL or Opcodes.ACC_SUPER or Opcodes.ACC_ENUM,
            className,
            null,
            "java/lang/Enum",
            null,
        )
        writer.visitMethod(Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC, "values", "()[L$className;", null, null).apply {
            visitCode()
            visitInsn(Opcodes.ACONST_NULL)
            visitInsn(Opcodes.ARETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitMethod(
            Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC,
            "valueOf",
            "(Ljava/lang/String;)L$className;",
            null,
            null,
        ).apply {
            visitCode()
            visitInsn(Opcodes.ACONST_NULL)
            visitInsn(Opcodes.ARETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitMethod(Opcodes.ACC_PUBLIC, "calculate", "()I", null, null).apply {
            visitCode()
            visitInsn(Opcodes.ICONST_1)
            visitInsn(Opcodes.IRETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private companion object {
        const val JANK_HUNTER_HOOKS = "io/jankhunter/runtime/JankHunterHooks"
    }

    private data class MethodStats(
        var parentCatchAllHandlers: Int = 0,
        var parentExitCalls: Int = 0,
        var childCatchAllHandlers: Int = 0,
        var childExitCalls: Int = 0,
        val enterDescriptors: MutableSet<String> = linkedSetOf(),
    )
}
