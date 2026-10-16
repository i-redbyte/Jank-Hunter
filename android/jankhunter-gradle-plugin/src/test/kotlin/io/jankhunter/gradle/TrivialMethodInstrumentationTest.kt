package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Opcodes
import org.objectweb.asm.tree.ClassNode
import org.objectweb.asm.tree.MethodInsnNode

class TrivialMethodInstrumentationTest {
    @Test
    fun filterRemovesOnlyGenericBoundariesAndHonorsExplicitOwner() {
        assertEquals(0, counters("get", "()I"))
        assertEquals(0, counters("get", "()I", dottedClassName = true))
        assertEquals(1, counters("get", "()I", annotated = true))
        assertEquals(1, counters("get", "()I", volatile = true))
        assertEquals(1, counters("get", "()I", mode = JankHunterMethodFilterMode.REPORT_ONLY))
        assertEquals(1, counters("get", "()I", mode = JankHunterMethodFilterMode.NONE))
        assertEquals(1, counters("run", "()V"))
    }

    private fun counters(
        name: String, descriptor: String, annotated: Boolean = false, volatile: Boolean = false,
        mode: JankHunterMethodFilterMode = JankHunterMethodFilterMode.FILTER,
        dottedClassName: Boolean = false,
    ): Int {
        val fixture = ClassWriter(0)
        fixture.visit(Opcodes.V1_8, Opcodes.ACC_PUBLIC, "test/Owner", null, "java/lang/Object", arrayOf("java/lang/Runnable"))
        fixture.visitField(Opcodes.ACC_PRIVATE or if (volatile) Opcodes.ACC_VOLATILE else 0, "value", "I", null, null).visitEnd()
        fixture.visitMethod(Opcodes.ACC_PUBLIC, name, descriptor, null, null).apply {
            if (annotated) visitAnnotation("Lio/jankhunter/annotations/JankHunterOwner;", false).apply {
                visit("value", "important")
                visitEnd()
            }
            visitCode()
            if (name == "run") {
                visitInsn(Opcodes.RETURN)
            } else {
                visitVarInsn(Opcodes.ALOAD, 0)
                visitFieldInsn(Opcodes.GETFIELD, "test/Owner", "value", "I")
                visitInsn(Opcodes.IRETURN)
            }
            visitMaxs(1, 1)
            visitEnd()
        }
        fixture.visitEnd()
        val reader = ClassReader(fixture.toByteArray())
        val output = ClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        val config = HookConfig(
            methodCounters = true, methodFilterMode = mode, okhttp = false, webSockets = false,
            handlers = false, executors = true, coroutines = false, interactionOperations = false,
            logSpam = false, classGraph = false, runtimeCallGraph = false, classGraphDirectory = "",
            instrumentationDiagnosticsDirectory = "", databaseTracing = false,
        )
        reader.accept(JankHunterClassVisitor(output,
            if (dottedClassName) reader.className.replace('/', '.') else reader.className, config), ClassReader.EXPAND_FRAMES)
        val transformed = ClassNode()
        ClassReader(output.toByteArray()).accept(transformed, 0)
        return transformed.methods.flatMap { it.instructions.toArray().filterIsInstance<MethodInsnNode>() }
            .count { it.owner == "io/jankhunter/runtime/JankHunterHooks" && it.name == "recordMethodCall" }
    }
}
