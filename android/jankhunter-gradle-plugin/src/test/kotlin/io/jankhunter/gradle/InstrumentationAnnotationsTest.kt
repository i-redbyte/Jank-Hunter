package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.AnnotationVisitor
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes

class InstrumentationAnnotationsTest {
    @Test
    fun classOwnerAnnotationOverridesGeneratedOwnerLabel() {
        val instrumented = instrument(ownerFixture(classOwner = "FeedOwner"))
        val strings = collectMethodStrings(instrumented, "load")

        assertTrue(strings.contains("owner.FeedOwner"))
        assertFalse(strings.any { it.startsWith("owner.example.Annotated.load#") })
    }

    @Test
    fun methodOwnerAnnotationOverridesClassOwnerLabel() {
        val instrumented = instrument(ownerFixture(classOwner = "ClassOwner", methodOwner = "MethodOwner"))
        val strings = collectMethodStrings(instrumented, "load")

        assertTrue(strings.contains("owner.MethodOwner"))
        assertFalse(strings.contains("owner.ClassOwner"))
    }

    @Test
    fun ignoreAnnotationSkipsMethodInstrumentation() {
        val instrumented = instrument(ownerFixture(methodIgnored = true))

        assertEquals(0, countRuntimeCalls(instrumented, "recordCounter"))
    }

    @Test
    fun classIgnoreAnnotationSkipsMethodInstrumentation() {
        val instrumented = instrument(ownerFixture(classIgnored = true))

        assertEquals(0, countRuntimeCalls(instrumented, "recordCounter"))
    }

    private fun instrument(bytes: ByteArray): ByteArray {
        val reader = ClassReader(bytes)
        val writer = ClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        reader.accept(
            JankHunterClassVisitor(
                writer,
                "example.Annotated",
                HookConfig(
                    methodCounters = true,
                    okhttp = false,
                    webSockets = false,
                    handlers = false,
                    executors = false,
                    coroutines = false,
                    flowInteractions = false,
                    logSpam = false,
                    classGraph = false,
                    runtimeCallGraph = false,
                    classGraphPath = "",
                ),
            ),
            ClassReader.EXPAND_FRAMES,
        )
        return writer.toByteArray()
    }

    private fun ownerFixture(
        classOwner: String? = null,
        methodOwner: String? = null,
        classIgnored: Boolean = false,
        methodIgnored: Boolean = false,
    ): ByteArray {
        val writer = ClassWriter(ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "example/Annotated", null, "java/lang/Object", null)
        if (classOwner != null) {
            writer.visitAnnotation(OWNER_DESCRIPTOR, false).stringValue(classOwner)
        }
        if (classIgnored) {
            writer.visitAnnotation(IGNORE_DESCRIPTOR, false).visitEnd()
        }

        writer.visitMethod(Opcodes.ACC_PUBLIC, "load", "()V", null, null).apply {
            if (methodOwner != null) {
                visitAnnotation(OWNER_DESCRIPTOR, false).stringValue(methodOwner)
            }
            if (methodIgnored) {
                visitAnnotation(IGNORE_DESCRIPTOR, false).visitEnd()
            }
            visitCode()
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }

        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun collectMethodStrings(bytes: ByteArray, targetMethod: String): List<String> {
        val strings = mutableListOf<String>()
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
                        override fun visitLdcInsn(value: Any?) {
                            if (value is String) strings.add(value)
                        }
                    }
                }
            },
            0,
        )
        return strings
    }

    private fun countRuntimeCalls(bytes: ByteArray, targetMethod: String): Int {
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
                            methodName: String,
                            descriptor: String,
                            isInterface: Boolean,
                        ) {
                            if (owner == "io/jankhunter/runtime/JankHunter" && methodName == targetMethod) {
                                count += 1
                            }
                        }
                    }
                }
            },
            0,
        )
        return count
    }

    private fun AnnotationVisitor.stringValue(value: String) {
        visit("value", value)
        visitEnd()
    }

    private companion object {
        private const val OWNER_DESCRIPTOR = "Lio/jankhunter/annotations/JankOwner;"
        private const val IGNORE_DESCRIPTOR = "Lio/jankhunter/annotations/JankIgnore;"
    }
}

