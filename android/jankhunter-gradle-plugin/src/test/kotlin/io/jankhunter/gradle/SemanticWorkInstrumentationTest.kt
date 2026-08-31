package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes

class SemanticWorkInstrumentationTest {
    @Test
    fun composableAnnotationAddsBalancedSemanticBoundary() {
        val calls = instrumentAndCollect(composeFixture(), config(compose = true))

        assertEquals(1, calls["render"]?.enters)
        assertEquals(2, calls["render"]?.exits)
        assertEquals(null, calls["regular"])
        assertEquals(null, calls["render${'$'}default"])
    }

    @Test
    fun roomDaoRequiresStructuralRoomDatabaseField() {
        val calls = instrumentAndCollect(roomFixture(withRoomField = true), config(room = true))
        val unrelated = instrumentAndCollect(roomFixture(withRoomField = false), config(room = true))

        assertEquals(1, calls["load"]?.enters)
        assertEquals(2, calls["load"]?.exits)
        assertEquals(null, calls["helper"])
        assertEquals(null, calls["getRequiredConverters"])
        assertEquals(emptyMap<String, SemanticCalls>(), unrelated)
    }

    @Test
    fun synchronousWorkerDoWorkIsMeasuredWithoutWorkManagerDependency() {
        val calls = instrumentAndCollect(
            workerFixture(),
            config(worker = true),
            hierarchy = setOf("example/SyncWorker", "androidx/work/Worker"),
        )

        assertEquals(0, calls["doWork"]?.enters)
        assertEquals(0, calls["doWork"]?.exits)
        assertEquals(1, calls["doWork"]?.workerInstanceIds)
        assertEquals(1, calls["doWork"]?.workerEnters)
        assertEquals(2, calls["doWork"]?.workerExits)
        assertEquals(1, calls["doWork"]?.workerOutcomeClassifications)
        assertTrue(calls["doWork"]?.stringConstants?.contains("example.SyncWorker.doWork") == true)
        assertEquals(null, calls["helper"])
    }

    private fun instrumentAndCollect(
        bytes: ByteArray,
        config: HookConfig,
        hierarchy: Set<String> = setOf(ClassReader(bytes).className),
    ): Map<String, SemanticCalls> {
        val reader = ClassReader(bytes)
        val writer = ClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        reader.accept(
            JankHunterClassVisitor(
                writer,
                reader.className,
                config,
                classHierarchy = hierarchy,
            ),
            ClassReader.EXPAND_FRAMES,
        )
        val calls = linkedMapOf<String, SemanticCalls>()
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
                        override fun visitLdcInsn(value: Any?) {
                            if (value is String) {
                                calls.getOrPut(name, ::SemanticCalls).stringConstants += value
                            }
                        }

                        override fun visitMethodInsn(
                            opcodeAndSource: Int,
                            owner: String,
                            methodName: String,
                            methodDescriptor: String,
                            isInterface: Boolean,
                        ) {
                            if (owner != JANK_HUNTER_HOOKS) return
                            val stats = calls.getOrPut(name, ::SemanticCalls)
                            if (methodName == "enterSemantic") stats.enters++
                            if (methodName == "exitSemantic") stats.exits++
                            if (methodName == "workerInstanceId") stats.workerInstanceIds++
                            if (methodName == "enterWorker") stats.workerEnters++
                            if (methodName == "exitWorker") stats.workerExits++
                            if (methodName == "classifyWorkerOutcome") stats.workerOutcomeClassifications++
                        }
                    }
                }
            },
            0,
        )
        return calls.filterValues { calls ->
            calls.enters > 0 || calls.exits > 0 || calls.workerInstanceIds > 0 ||
                calls.workerEnters > 0 || calls.workerExits > 0
        }
    }

    private fun config(compose: Boolean = false, room: Boolean = false, worker: Boolean = false): HookConfig {
        return HookConfig(
            methodCounters = false,
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
            composeTracing = compose,
            roomTracing = room,
            workerTracing = worker,
        )
    }

    private fun composeFixture(): ByteArray {
        val writer = classWriter("example/ComposeScreen")
        writer.voidMethod("render") {
            visitAnnotation(COMPOSABLE_DESCRIPTOR, false)?.visitEnd()
        }
        writer.voidMethod("render${'$'}default", Opcodes.ACC_PUBLIC or Opcodes.ACC_SYNTHETIC) {
            visitAnnotation(COMPOSABLE_DESCRIPTOR, false)?.visitEnd()
        }
        writer.voidMethod("regular")
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun roomFixture(withRoomField: Boolean): ByteArray {
        val writer = classWriter("example/UserDao_Impl")
        if (withRoomField) {
            writer.visitField(
                Opcodes.ACC_PRIVATE,
                "database",
                "Landroidx/room/RoomDatabase;",
                null,
                null,
            )?.visitEnd()
        }
        writer.voidMethod("load")
        writer.voidMethod("helper", Opcodes.ACC_PRIVATE)
        writer.voidMethod("getRequiredConverters", Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC)
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun workerFixture(): ByteArray {
        val writer = classWriter("example/SyncWorker", "androidx/work/Worker")
        writer.visitMethod(
            Opcodes.ACC_PUBLIC,
            "doWork",
            "()Landroidx/work/ListenableWorker${'$'}Result;",
            null,
            null,
        ).apply {
            visitCode()
            visitInsn(Opcodes.ACONST_NULL)
            visitInsn(Opcodes.ARETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.voidMethod("helper")
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun classWriter(name: String, parent: String = "java/lang/Object"): ClassWriter {
        return ClassWriter(ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS).apply {
            visit(Opcodes.V17, Opcodes.ACC_PUBLIC, name, null, parent, null)
        }
    }

    private fun ClassWriter.voidMethod(
        name: String,
        access: Int = Opcodes.ACC_PUBLIC,
        annotation: (MethodVisitor.() -> Unit)? = null,
    ) {
        visitMethod(access, name, "()V", null, null).apply {
            annotation?.invoke(this)
            visitCode()
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
    }

    private data class SemanticCalls(
        var enters: Int = 0,
        var exits: Int = 0,
        var workerInstanceIds: Int = 0,
        var workerEnters: Int = 0,
        var workerExits: Int = 0,
        var workerOutcomeClassifications: Int = 0,
        val stringConstants: MutableSet<String> = linkedSetOf(),
    )

    private companion object {
        const val JANK_HUNTER_HOOKS = "io/jankhunter/runtime/JankHunterHooks"
        const val COMPOSABLE_DESCRIPTOR = "Landroidx/compose/runtime/Composable;"
    }
}
