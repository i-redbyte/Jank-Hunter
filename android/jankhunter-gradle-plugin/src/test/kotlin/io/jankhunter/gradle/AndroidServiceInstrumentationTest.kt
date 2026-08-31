package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes

class AndroidServiceInstrumentationTest {
    @Test
    fun policyRecognizesOnlyRealServiceCallbacks() {
        assertEquals(
            AndroidServiceCallback.START_COMMAND,
            AndroidServiceInstrumentationPolicy.callback(
                "onStartCommand",
                "(Landroid/content/Intent;II)I",
            ),
        )
        assertEquals(
            AndroidServiceCallback.TIMEOUT,
            AndroidServiceInstrumentationPolicy.callback("onTimeout", "(II)V"),
        )
        assertNull(AndroidServiceInstrumentationPolicy.callback("onStartCommand", "()I"))
        assertNull(AndroidServiceInstrumentationPolicy.callback("helper", "()V"))
    }

    @Test
    fun serviceCallbacksHaveBalancedSuccessAndFailureHooks() {
        val methods = instrumentAndCollect(serviceFixture())

        assertEquals(1, methods.getValue("onCreate").enters)
        assertEquals(2, methods.getValue("onCreate").exits)
        assertEquals(1, methods.getValue("onStartCommand").enters)
        assertEquals(2, methods.getValue("onStartCommand").exits)
        assertEquals(1, methods.getValue("onBind").enters)
        assertEquals(2, methods.getValue("onBind").exits)
        assertEquals(1, methods.getValue("onDestroy").enters)
        assertEquals(2, methods.getValue("onDestroy").exits)
        assertNull(methods["helper"])
    }

    @Test
    fun foregroundTransitionsAreRecordedAfterSuccessfulFrameworkCalls() {
        val methods = instrumentAndCollect(serviceFixture())

        assertEquals(2, methods.getValue("onStartCommand").foregroundTransitions)
        assertTrue(methods.getValue("onStartCommand").hookStages.contains(COMPONENT_SERVICE_FOREGROUND_ENTERED))
        assertTrue(methods.getValue("onStartCommand").hookStages.contains(COMPONENT_SERVICE_FOREGROUND_EXITED))
        assertEquals(
            listOf("framework:startForeground", "hook:foreground", "framework:stopForeground", "hook:foreground"),
            methods.getValue("onStartCommand").foregroundOrder,
        )
    }

    private fun instrumentAndCollect(bytes: ByteArray): Map<String, ServiceCalls> {
        val reader = ClassReader(bytes)
        val writer = object : ClassWriter(reader, COMPUTE_FRAMES or COMPUTE_MAXS) {
            override fun getCommonSuperClass(type1: String?, type2: String?): String = "java/lang/Object"
        }
        reader.accept(
            JankHunterClassVisitor(
                writer,
                reader.className,
                config(),
                classHierarchy = setOf(reader.className, ANDROID_SERVICE),
                resolveOwnerHierarchy = { owner ->
                    if (owner == ANDROID_SERVICE) setOf(owner) else setOf(owner)
                },
            ),
            ClassReader.EXPAND_FRAMES,
        )
        val calls = linkedMapOf<String, ServiceCalls>()
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
                        private var lastInteger: Int? = null

                        override fun visitInsn(opcode: Int) {
                            lastInteger = when (opcode) {
                                Opcodes.ICONST_0 -> 0
                                Opcodes.ICONST_1 -> 1
                                Opcodes.ICONST_2 -> 2
                                Opcodes.ICONST_3 -> 3
                                Opcodes.ICONST_4 -> 4
                                Opcodes.ICONST_5 -> 5
                                else -> lastInteger
                            }
                        }

                        override fun visitIntInsn(opcode: Int, operand: Int) {
                            if (opcode == Opcodes.BIPUSH || opcode == Opcodes.SIPUSH) lastInteger = operand
                        }

                        override fun visitLdcInsn(value: Any?) {
                            if (value is Int) lastInteger = value
                        }

                        override fun visitMethodInsn(
                            opcodeAndSource: Int,
                            owner: String,
                            methodName: String,
                            methodDescriptor: String,
                            isInterface: Boolean,
                        ) {
                            val stats = calls.getOrPut(name, ::ServiceCalls)
                            if (owner == ANDROID_SERVICE &&
                                (methodName == "startForeground" || methodName == "stopForeground")
                            ) {
                                stats.foregroundOrder += "framework:$methodName"
                                return
                            }
                            if (owner != JANK_HUNTER_ANDROID_HOOKS) return
                            when (methodName) {
                                "enterServiceCallback" -> stats.enters++
                                "exitServiceCallback" -> stats.exits++
                                "recordServiceForegroundTransition" -> {
                                    stats.foregroundTransitions++
                                    stats.foregroundOrder += "hook:foreground"
                                    lastInteger?.let(stats.hookStages::add)
                                }
                            }
                        }
                    }
                }
            },
            0,
        )
        return calls
    }

    private fun serviceFixture(): ByteArray {
        val writer = ClassWriter(ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "example/SyncService", null, ANDROID_SERVICE, null)
        writer.method(Opcodes.ACC_PUBLIC, "onCreate", "()V") {
            visitInsn(Opcodes.RETURN)
        }
        writer.method(Opcodes.ACC_PUBLIC, "onStartCommand", "(Landroid/content/Intent;II)I") {
            visitVarInsn(Opcodes.ALOAD, 0)
            visitInsn(Opcodes.ICONST_1)
            visitInsn(Opcodes.ACONST_NULL)
            visitMethodInsn(
                Opcodes.INVOKEVIRTUAL,
                ANDROID_SERVICE,
                "startForeground",
                "(ILandroid/app/Notification;)V",
                false,
            )
            visitVarInsn(Opcodes.ALOAD, 0)
            visitInsn(Opcodes.ICONST_1)
            visitMethodInsn(Opcodes.INVOKEVIRTUAL, ANDROID_SERVICE, "stopForeground", "(Z)V", false)
            visitInsn(Opcodes.ICONST_1)
            visitInsn(Opcodes.IRETURN)
        }
        writer.method(
            Opcodes.ACC_PUBLIC,
            "onBind",
            "(Landroid/content/Intent;)Landroid/os/IBinder;",
        ) {
            visitInsn(Opcodes.ACONST_NULL)
            visitInsn(Opcodes.ARETURN)
        }
        writer.method(Opcodes.ACC_PUBLIC, "onDestroy", "()V") {
            visitInsn(Opcodes.RETURN)
        }
        writer.method(Opcodes.ACC_PRIVATE, "helper", "()V") {
            visitInsn(Opcodes.RETURN)
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun ClassWriter.method(
        access: Int,
        name: String,
        descriptor: String,
        body: MethodVisitor.() -> Unit,
    ) {
        visitMethod(access, name, descriptor, null, null).apply {
            visitCode()
            body()
            visitMaxs(0, 0)
            visitEnd()
        }
    }

    private fun config(): HookConfig = HookConfig(
        methodCounters = false,
        methodFilterMode = JankHunterMethodFilterMode.NONE,
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
        androidComponents = true,
    )

    private data class ServiceCalls(
        var enters: Int = 0,
        var exits: Int = 0,
        var foregroundTransitions: Int = 0,
        val hookStages: MutableList<Int> = mutableListOf(),
        val foregroundOrder: MutableList<String> = mutableListOf(),
    )

    private companion object {
        const val ANDROID_SERVICE = "android/app/Service"
        const val JANK_HUNTER_ANDROID_HOOKS = "io/jankhunter/runtime/JankHunterAndroidHooks"
        const val COMPONENT_SERVICE_FOREGROUND_ENTERED = 7
        const val COMPONENT_SERVICE_FOREGROUND_EXITED = 8
    }
}
