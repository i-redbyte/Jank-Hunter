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
import java.nio.file.Files

class InstrumentationAnnotationsTest {
    @Test
    fun classOwnerAnnotationOverridesGeneratedOwnerLabel() {
        val instrumented = instrument(ownerFixture(classOwner = "FeedOwner"))
        val strings = collectMethodStrings(instrumented, "load")

        assertTrue(strings.contains("FeedOwner"))
        assertFalse(strings.any { it.startsWith("example.Annotated.load#") })
    }

    @Test
    fun methodOwnerAnnotationOverridesClassOwnerLabel() {
        val instrumented = instrument(ownerFixture(classOwner = "ClassOwner", methodOwner = "MethodOwner"))
        val strings = collectMethodStrings(instrumented, "load")

        assertTrue(strings.contains("MethodOwner"))
        assertFalse(strings.contains("ClassOwner"))
    }

    @Test
    fun ignoreAnnotationSkipsMethodInstrumentation() {
        val instrumented = instrument(ownerFixture(methodIgnored = true))

        assertEquals(0, countRuntimeCallsInMethod(instrumented, "load", "recordMethodCall"))
    }

    @Test
    fun classIgnoreAnnotationSkipsMethodInstrumentation() {
        val instrumented = instrument(ownerFixture(classIgnored = true))

        assertEquals(0, countRuntimeCalls(instrumented, "recordMethodCall"))
    }

    @Test
    fun ignoreAnnotationSkipsClassGraphEdges() {
        val graph = Files.createTempDirectory("jankhunter-ignored-graph").toFile()

        instrument(
            ownerFixture(
                methodIgnored = true,
                calleeOwner = "com/example/FeedRepository",
            ),
            classGraphDirectory = graph.absolutePath,
        )

        assertTrue(InstrumentationArtifactFiles.readJsonlLines(graph).isEmpty())
    }

    @Test
    fun classGraphKeepsEdgesForInstrumentedMethods() {
        val graph = Files.createTempDirectory("jankhunter-class-graph").toFile()

        instrument(
            ownerFixture(calleeOwner = "com/example/FeedRepository"),
            classGraphDirectory = graph.absolutePath,
        )

        val text = InstrumentationArtifactFiles.readJsonlLines(graph).joinToString("\n")
        assertTrue(text.contains("\"calleeClass\":\"com.example.FeedRepository\""))
    }

    @Test
    fun screenAndOperationAnnotationsEnterRuntimeContextAndOperation() {
        val instrumented = instrument(
            ownerFixture(
                classScreen = "FeedScreen",
                classOperation = "feed.open",
                methodOperation = "refresh",
            ),
        )

        val strings = collectMethodStrings(instrumented, "load")
        assertTrue(strings.contains("FeedScreen"))
        assertTrue(strings.contains("FeedOwner"))
        assertTrue(strings.contains("refresh"))
        assertEquals(1, countRuntimeCalls(instrumented, "enterAnnotatedContext"))
        assertEquals(2, countRuntimeCalls(instrumented, "exitAnnotatedContext"))
        assertEquals(1, countRuntimeCalls(instrumented, "startAnnotatedOperation"))
        assertEquals(2, countRuntimeCalls(instrumented, "finishAnnotatedOperation"))
    }

    @Test
    fun blankOperationAnnotationIsIgnored() {
        val instrumented = instrument(ownerFixture(methodOperation = ""))

        val strings = collectMethodStrings(instrumented, "load")

        assertFalse(strings.contains("load"))
        assertEquals(0, countRuntimeCalls(instrumented, "startAnnotatedOperation"))
    }

    @Test
    fun declaredConstructorsReceiveBoundaryAndAnnotationHooksAfterSuper() {
        val instrumented = instrument(ownerFixture(constructorOperation = "create"))

        val strings = collectMethodStrings(instrumented, "<init>")

        assertTrue(strings.contains("create"))
        assertTrue(strings.contains("FeedOwner"))
        assertEquals(1, countRuntimeCallsInMethod(instrumented, "<init>", "recordMethodCall"))
        assertEquals(1, countRuntimeCallsInMethod(instrumented, "<init>", "enterAnnotatedContext"))
        assertEquals(2, countRuntimeCallsInMethod(instrumented, "<init>", "exitAnnotatedContext"))
        assertEquals(1, countRuntimeCallsInMethod(instrumented, "<init>", "startAnnotatedOperation"))
        assertEquals(2, countRuntimeCallsInMethod(instrumented, "<init>", "finishAnnotatedOperation"))
        assertEquals(1, countCatchAllHandlers(instrumented, "<init>"))
    }

    @Test
    fun classInitializersRemainUntouched() {
        val instrumented = instrument(ownerFixture())

        assertEquals(0, countRuntimeCallsInMethod(instrumented, "<clinit>", "recordMethodCall"))
    }

    @Test
    fun annotatedContextGetsCatchAllExitForExceptionUnwind() {
        val instrumented = instrument(ownerFixture(methodOperation = "refresh", throwsException = true))

        assertEquals(1, countCatchAllHandlers(instrumented, "load"))
        assertEquals(1, countRuntimeCalls(instrumented, "exitAnnotatedContext"))
        assertEquals(1, countRuntimeCalls(instrumented, "finishAnnotatedOperation"))
    }

    private fun instrument(bytes: ByteArray, classGraphDirectory: String = ""): ByteArray {
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
                    interactionOperations = false,
                    logSpam = false,
                    classGraph = classGraphDirectory.isNotBlank(),
                    runtimeCallGraph = false,
                    classGraphDirectory = classGraphDirectory,
                    instrumentationDiagnosticsDirectory = "",
                ),
            ),
            ClassReader.EXPAND_FRAMES,
        )
        return writer.toByteArray()
    }

    private fun ownerFixture(
        classOwner: String? = null,
        methodOwner: String? = null,
        classScreen: String? = null,
        classOperation: String? = null,
        methodOperation: String? = null,
        constructorOperation: String? = null,
        classIgnored: Boolean = false,
        methodIgnored: Boolean = false,
        throwsException: Boolean = false,
        calleeOwner: String? = null,
    ): ByteArray {
        val writer = ClassWriter(ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "example/Annotated", null, "java/lang/Object", null)
        writer.visitAnnotation(OWNER_DESCRIPTOR, false).finishStringValue(classOwner ?: "FeedOwner")
        if (classScreen != null) {
            writer.visitAnnotation(SCREEN_DESCRIPTOR, false).finishStringValue(classScreen)
        }
        if (classOperation != null) {
            writer.visitAnnotation(OPERATION_DESCRIPTOR, false).finishStringValue(classOperation)
        }
        if (classIgnored) {
            writer.visitAnnotation(IGNORE_DESCRIPTOR, false).visitEnd()
        }

        writer.visitMethod(Opcodes.ACC_STATIC, "<clinit>", "()V", null, null).apply {
            visitCode()
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }

        writer.visitMethod(Opcodes.ACC_PUBLIC, "<init>", "()V", null, null).apply {
            if (constructorOperation != null) {
                visitAnnotation(OPERATION_DESCRIPTOR, false).finishStringValue(constructorOperation)
            }
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            visitMethodInsn(Opcodes.INVOKESPECIAL, "java/lang/Object", "<init>", "()V", false)
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }

        writer.visitMethod(Opcodes.ACC_PUBLIC, "load", "()V", null, null).apply {
            if (methodOwner != null) {
                visitAnnotation(OWNER_DESCRIPTOR, false).finishStringValue(methodOwner)
            }
            if (methodOperation != null) {
                val annotation = visitAnnotation(OPERATION_DESCRIPTOR, false)
                if (methodOperation.isNotEmpty()) {
                    annotation.finishStringValue(methodOperation)
                } else {
                    annotation.visitEnd()
                }
            }
            if (methodIgnored) {
                visitAnnotation(IGNORE_DESCRIPTOR, false).visitEnd()
            }
            visitCode()
            if (throwsException) {
                visitTypeInsn(Opcodes.NEW, "java/lang/RuntimeException")
                visitInsn(Opcodes.DUP)
                visitMethodInsn(Opcodes.INVOKESPECIAL, "java/lang/RuntimeException", "<init>", "()V", false)
                visitInsn(Opcodes.ATHROW)
            } else {
                if (calleeOwner != null) {
                    visitMethodInsn(Opcodes.INVOKESTATIC, calleeOwner, "refresh", "()V", false)
                }
                visitInsn(Opcodes.RETURN)
            }
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
                            if (owner == "io/jankhunter/runtime/JankHunterHooks" && methodName == targetMethod) {
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

    private fun countRuntimeCallsInMethod(bytes: ByteArray, targetMethod: String, runtimeMethod: String): Int {
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
                    if (name != targetMethod) {
                        return object : MethodVisitor(Opcodes.ASM9) {}
                    }
                    return object : MethodVisitor(Opcodes.ASM9) {
                        override fun visitMethodInsn(
                            opcodeAndSource: Int,
                            owner: String,
                            methodName: String,
                            descriptor: String,
                            isInterface: Boolean,
                        ) {
                            if (owner == "io/jankhunter/runtime/JankHunterHooks" &&
                                methodName == runtimeMethod
                            ) {
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

    private fun countCatchAllHandlers(bytes: ByteArray, targetMethod: String): Int {
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
                        override fun visitTryCatchBlock(
                            start: org.objectweb.asm.Label,
                            end: org.objectweb.asm.Label,
                            handler: org.objectweb.asm.Label,
                            type: String?,
                        ) {
                            if (type == null) count += 1
                        }
                    }
                }
            },
            0,
        )
        return count
    }

    private companion object {
        private const val OWNER_DESCRIPTOR = "Lio/jankhunter/annotations/JankHunterOwner;"
        private const val SCREEN_DESCRIPTOR = "Lio/jankhunter/annotations/JankHunterScreen;"
        private const val OPERATION_DESCRIPTOR = "Lio/jankhunter/annotations/JankHunterOperation;"
        private const val IGNORE_DESCRIPTOR = "Lio/jankhunter/annotations/JankHunterIgnore;"
    }
}
