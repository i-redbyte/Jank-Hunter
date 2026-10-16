package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Label
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes
import org.objectweb.asm.tree.ClassNode
import org.objectweb.asm.tree.IntInsnNode
import org.objectweb.asm.tree.LdcInsnNode
import org.objectweb.asm.tree.MethodInsnNode

class CausalBridgeInstrumentationTest {
    @Test fun resourceWrapperUsesStaticMetadataOverload() {
        val calls = instrumentCalls("resource") { method ->
            method.visitFieldInsn(Opcodes.GETSTATIC, RESOURCES, "INSTANCE", "L$RESOURCES;")
            method.visitVarInsn(Opcodes.ALOAD, 0)
            method.visitVarInsn(Opcodes.ALOAD, 1)
            method.visitMethodInsn(Opcodes.INVOKEVIRTUAL, RESOURCES, "withLock",
                "(Ljava/util/concurrent/locks/Lock;Lkotlin/jvm/functions/Function0;)Ljava/lang/Object;", false)
            method.visitInsn(Opcodes.ARETURN)
        }
        val resourceCalls = calls.filter { it.owner == RESOURCES }
        assertEquals(1, resourceCalls.size)
        assertEquals(Opcodes.INVOKESTATIC, resourceCalls.single().opcode)
        assertEquals("(Ljava/util/concurrent/locks/Lock;Lkotlin/jvm/functions/Function0;" +
            "JLjava/lang/String;II)Ljava/lang/Object;", resourceCalls.single().descriptor)
    }

    @Test fun generatedDefaultAcquireKeepsMaskAndUsesSourceAwareBridge() {
        val calls = instrumentCalls("acquire") { method ->
            method.visitVarInsn(Opcodes.ALOAD, 0)
            method.visitInsn(Opcodes.ICONST_0)
            method.visitInsn(Opcodes.ICONST_2)
            method.visitInsn(Opcodes.ACONST_NULL)
            method.visitMethodInsn(Opcodes.INVOKESTATIC, RESOURCES, "beginAcquire\$default",
                "(Lio/jankhunter/runtime/JankHunterResourceKind;ZILjava/lang/Object;)" +
                    "Lio/jankhunter/runtime/JankHunterResourceLease;", false)
            method.visitInsn(Opcodes.ARETURN)
        }
        val resourceCalls = calls.filter { it.owner == RESOURCES }
        assertEquals(listOf("beginAcquireWithDefaults"), resourceCalls.map { it.name })
        assertEquals("(Lio/jankhunter/runtime/JankHunterResourceKind;ZIJLjava/lang/String;II)" +
            "Lio/jankhunter/runtime/JankHunterResourceLease;", resourceCalls.single().descriptor)
    }

    @Test fun plainThreadTargetIsPreparedAndBoundAfterOneConstructorCall() {
        val calls = instrumentCalls("thread") { method ->
            method.visitTypeInsn(Opcodes.NEW, "java/lang/Thread")
            method.visitInsn(Opcodes.DUP)
            method.visitVarInsn(Opcodes.ALOAD, 0)
            method.visitMethodInsn(Opcodes.INVOKESPECIAL, "java/lang/Thread", "<init>",
                "(Ljava/lang/Runnable;)V", false)
            method.visitInsn(Opcodes.ARETURN)
        }
        val names = calls.map { it.name }
        assertEquals(1, calls.count { it.owner == "java/lang/Thread" && it.name == "<init>" })
        assertTrue(names.indexOf("prepareThreadTarget") < names.indexOf("<init>"))
        assertTrue(names.indexOf("<init>") < names.indexOf("bindThreadTarget"))
        assertTrue(names.indexOf("bindThreadTarget") < names.indexOf("observeThreadConstructed"))
        assertEquals(1, names.count { it == "observeThreadConstructed" })
    }

    @Test fun nullThreadTargetStillRecordsSuccessfulConstructor() {
        val calls = instrumentCalls("nullThread") { method ->
            method.visitTypeInsn(Opcodes.NEW, "java/lang/Thread")
            method.visitInsn(Opcodes.DUP)
            method.visitInsn(Opcodes.ACONST_NULL)
            method.visitMethodInsn(Opcodes.INVOKESPECIAL, "java/lang/Thread", "<init>",
                "(Ljava/lang/Runnable;)V", false)
            method.visitInsn(Opcodes.ARETURN)
        }
        assertEquals(1, calls.count { it.owner == "java/lang/Thread" && it.name == "<init>" })
        assertEquals(1, calls.count { it.name == "bindThreadTarget" })
        assertEquals(1, calls.count { it.name == "observeThreadConstructed" })
    }

    @Test fun successfulJoinAndIsAliveProbeTerminationWithoutReplacingTheCall() {
        val joined = instrumentCalls("join") { method ->
            method.visitVarInsn(Opcodes.ALOAD, 0)
            method.visitMethodInsn(Opcodes.INVOKEVIRTUAL, "java/lang/Thread", "join", "()V", false)
            method.visitInsn(Opcodes.RETURN)
        }.filter { it.owner == "java/lang/Thread" || it.name == "observeThreadEnd" }
        assertEquals(listOf("join", "observeThreadEnd"), joined.map(Call::name))

        val alive = instrumentCalls("alive") { method ->
            method.visitVarInsn(Opcodes.ALOAD, 0)
            method.visitMethodInsn(Opcodes.INVOKEVIRTUAL, "java/lang/Thread", "isAlive", "()Z", false)
            method.visitInsn(Opcodes.IRETURN)
        }.filter { it.owner == "java/lang/Thread" || it.name == "observeThreadEnd" }
        assertEquals(listOf("isAlive", "observeThreadEnd"), alive.map(Call::name))
    }

    @Test fun applicationWorkerOverridesReportTheirOwnImplementationSite() {
        val sync = instrumentWorkerImplementation(
            "()Landroidx/work/ListenableWorker\$Result;",
            "io/jankhunter/workmanager/JankHunterWorker",
            42,
        )
        val suspendWorker = instrumentWorkerImplementation(
            "(Lkotlin/coroutines/Continuation;)Ljava/lang/Object;",
            "io/jankhunter/workmanager/JankHunterCoroutineWorker",
            42,
        )
        val noLine = instrumentWorkerImplementation(
            "(Lkotlin/coroutines/Continuation;)Ljava/lang/Object;",
            "io/jankhunter/workmanager/JankHunterCoroutineWorker",
            null,
        )
        val unrelated = instrumentWorkerImplementation(
            "(Lkotlin/coroutines/Continuation;)Ljava/lang/Object;",
            "java/lang/Object",
            42,
        )

        assertTrue(sync.sourceLines.contains(42))
        assertTrue(suspendWorker.sourceLines.contains(42))
        assertTrue(noLine.sourceLines.isNotEmpty())
        assertTrue(noLine.sourceLines.all { it == 0 })
        assertTrue(unrelated.sourceLines.isEmpty())
        for (result in listOf(sync, suspendWorker, noLine, unrelated)) {
            assertEquals(0, result.legacyWorkerEnters)
        }
        assertTrue(suspendWorker.sourceMethods.contains(
            "example.AppWorker#doJankHunterWork(Lkotlin/coroutines/Continuation;)Ljava/lang/Object;",
        ))
    }

    private fun instrumentWorkerImplementation(descriptor: String, ancestor: String, line: Int?): WorkerSiteCalls {
        val original = ClassWriter(ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        original.visit(Opcodes.V1_8, Opcodes.ACC_PUBLIC, "example/AppWorker", null, "java/lang/Object", null)
        original.visitMethod(Opcodes.ACC_PUBLIC, "doJankHunterWork", descriptor, null, null).apply {
            visitCode()
            if (line != null) {
                val label = Label()
                visitLabel(label)
                visitLineNumber(line, label)
            }
            visitInsn(Opcodes.ACONST_NULL)
            visitInsn(Opcodes.ARETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        original.visitEnd()
        val reader = ClassReader(original.toByteArray())
        val writer = ClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        val config = HookConfig(
            methodCounters = false, okhttp = false, webSockets = false, handlers = false,
            executors = false, concurrencyDiagnostics = true, coroutines = false,
            interactionOperations = false, logSpam = false, classGraph = false,
            runtimeCallGraph = false, classGraphDirectory = "", instrumentationDiagnosticsDirectory = "",
        )
        reader.accept(JankHunterClassVisitor(
            writer, reader.className, config, classOrigin = CausalCodeOrigin.PROJECT,
            classHierarchy = setOf(reader.className, ancestor),
        ), ClassReader.EXPAND_FRAMES)
        val transformed = ClassNode()
        ClassReader(writer.toByteArray()).accept(transformed, 0)
        val instructions = transformed.methods.single { it.name == "doJankHunterWork" }.instructions.toArray().toList()
        val siteCalls = instructions.withIndex().filter { (_, instruction) ->
            instruction is MethodInsnNode && instruction.owner == HOOKS &&
                instruction.name == "observeWorkerImplementationSite"
        }
        val sourceLines = siteCalls.map { (index, _) ->
            val site = instructions.subList((index - 5).coerceAtLeast(0), index)
            if (site.any { it is IntInsnNode && it.operand == 42 }) 42 else 0
        }
        val sourceMethods = instructions.filterIsInstance<LdcInsnNode>()
            .mapNotNull { it.cst as? String }
        val legacyWorkerEnters = instructions.filterIsInstance<MethodInsnNode>()
            .count { it.owner == HOOKS && it.name == "enterWorker" }
        return WorkerSiteCalls(sourceLines, sourceMethods, legacyWorkerEnters)
    }

    private fun instrumentCalls(kind: String, body: (MethodVisitor) -> Unit): List<Call> {
        val original = ClassWriter(ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        original.visit(Opcodes.V1_8, Opcodes.ACC_PUBLIC, "example/CausalCaller", null, "java/lang/Object", null)
        val descriptor = when (kind) {
            "resource" -> "(Ljava/util/concurrent/locks/Lock;Lkotlin/jvm/functions/Function0;)Ljava/lang/Object;"
            "acquire" -> "(Lio/jankhunter/runtime/JankHunterResourceKind;)" +
                "Lio/jankhunter/runtime/JankHunterResourceLease;"
            "join" -> "(Ljava/lang/Thread;)V"
            "alive" -> "(Ljava/lang/Thread;)Z"
            else -> "(Ljava/lang/Runnable;)Ljava/lang/Thread;"
        }
        original.visitMethod(Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC, kind, descriptor, null, null).apply {
            visitCode()
            val line = Label()
            visitLabel(line)
            visitLineNumber(42, line)
            body(this)
            visitMaxs(0, 0)
            visitEnd()
        }
        original.visitEnd()
        val reader = ClassReader(original.toByteArray())
        val writer = ClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        val config = HookConfig(
            methodCounters = false, okhttp = false, webSockets = false, handlers = false,
            executors = false, concurrencyDiagnostics = true, coroutines = false,
            interactionOperations = false, logSpam = false, classGraph = false,
            runtimeCallGraph = false, classGraphDirectory = "", instrumentationDiagnosticsDirectory = "",
        )
        reader.accept(JankHunterClassVisitor(writer, reader.className, config,
            classOrigin = CausalCodeOrigin.PROJECT), ClassReader.EXPAND_FRAMES)
        val calls = mutableListOf<Call>()
        ClassReader(writer.toByteArray()).accept(object : ClassVisitor(Opcodes.ASM9) {
            override fun visitMethod(access: Int, name: String, descriptor: String, signature: String?,
                exceptions: Array<out String>?): MethodVisitor = object : MethodVisitor(Opcodes.ASM9) {
                override fun visitMethodInsn(opcode: Int, owner: String, name: String,
                    descriptor: String, isInterface: Boolean) {
                    calls += Call(opcode, owner, name, descriptor)
                }
            }
        }, 0)
        return calls
    }

    private data class Call(val opcode: Int, val owner: String, val name: String, val descriptor: String)

    private data class WorkerSiteCalls(
        val sourceLines: List<Int>,
        val sourceMethods: List<String>,
        val legacyWorkerEnters: Int,
    )

    private companion object {
        const val RESOURCES = "io/jankhunter/runtime/JankHunterResources"
        const val HOOKS = "io/jankhunter/runtime/JankHunterHooks"
    }
}
