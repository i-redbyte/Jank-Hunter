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

class AndroidBinderInstrumentationTest {
    @Test
    fun policyMatchesExactServerAndClientBinderApis() {
        assertEquals(
            AndroidBinderInvocation.SERVER_TRANSACTION,
            AndroidBinderInstrumentationPolicy.serverCallback(
                "onTransact",
                "(ILandroid/os/Parcel;Landroid/os/Parcel;I)Z",
            ),
        )
        assertEquals(
            AndroidBinderInvocation.CLIENT_TRANSACTION,
            AndroidBinderInstrumentationPolicy.clientInvocation(
                "android/os/IBinder",
                "transact",
                "(ILandroid/os/Parcel;Landroid/os/Parcel;I)Z",
            ),
        )
        assertNull(AndroidBinderInstrumentationPolicy.serverCallback("onTransact", "()Z"))
        assertNull(AndroidBinderInstrumentationPolicy.clientInvocation("android/os/IBinder", "transact", "()Z"))
        assertNull(AndroidBinderInstrumentationPolicy.runtimeDescriptor("example/BinderHelper", null))
        assertNull(AndroidBinderInstrumentationPolicy.runtimeMethod("example/BinderHelper", "send"))
    }

    @Test
    fun serverOnTransactHasBalancedSuccessAndFailureHooks() {
        val output = instrument(
            binderServerFixture(),
            setOf("example/ISync\$Stub", "android/os/Binder"),
        )
        val calls = collect(output)

        assertEquals(1, calls.getValue("onTransact").serverEnters)
        assertEquals(2, calls.getValue("onTransact").serverExits)
        assertTrue(calls.getValue("onTransact").strings.contains("example.ISync"))
    }

    @Test
    fun clientTransactRecordsFailureBeforeRethrowAndKeepsOriginalResult() {
        val output = instrument(
            binderProxyFixture(),
            setOf("example/ISync\$Stub\$Proxy", "example/ISync", "android/os/IInterface"),
        )
        val calls = collect(output).getValue("syncNow")

        assertEquals(1, calls.clientEnters)
        assertEquals(2, calls.clientExits)
        assertEquals(1, calls.frameworkTransacts)
        assertTrue(calls.strings.contains("example.ISync"))
        assertTrue(calls.strings.contains("syncNow"))
    }

    @Test
    fun componentLifecycleFeatureDoesNotImplicitlyEnableBinderInstrumentation() {
        val output = instrument(
            binderProxyFixture(),
            setOf("example/ISync\$Stub\$Proxy", "example/ISync", "android/os/IInterface"),
            config = config(androidComponents = true, binderIPC = false),
        )

        assertTrue(collect(output).isEmpty())
    }

    private fun instrument(
        bytes: ByteArray,
        hierarchy: Set<String>,
        config: HookConfig = config(),
    ): ByteArray {
        val reader = ClassReader(bytes)
        val writer = object : ClassWriter(reader, COMPUTE_FRAMES or COMPUTE_MAXS) {
            override fun getCommonSuperClass(type1: String?, type2: String?): String = "java/lang/Object"
        }
        reader.accept(
            JankHunterClassVisitor(
                writer,
                reader.className,
                config,
                classHierarchy = hierarchy,
                resolveOwnerHierarchy = { owner ->
                    if (owner == "android/os/IBinder") setOf(owner) else setOf(owner)
                },
            ),
            ClassReader.EXPAND_FRAMES,
        )
        return writer.toByteArray()
    }

    private fun collect(bytes: ByteArray): Map<String, BinderCalls> {
        val result = linkedMapOf<String, BinderCalls>()
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
                        override fun visitLdcInsn(value: Any?) {
                            if (value is String) result.getOrPut(name, ::BinderCalls).strings += value
                        }

                        override fun visitMethodInsn(
                            opcodeAndSource: Int,
                            owner: String,
                            methodName: String,
                            methodDescriptor: String,
                            isInterface: Boolean,
                        ) {
                            val calls = result.getOrPut(name, ::BinderCalls)
                            if (owner == "android/os/IBinder" && methodName == "transact") {
                                calls.frameworkTransacts++
                            }
                            if (owner != JANK_HUNTER_ANDROID_HOOKS) return
                            when (methodName) {
                                "enterBinderServer" -> calls.serverEnters++
                                "exitBinderServer" -> calls.serverExits++
                                "enterBinderClient" -> calls.clientEnters++
                                "exitBinderClient" -> calls.clientExits++
                            }
                        }
                    }
                }
            },
            0,
        )
        return result.filterValues { calls ->
            calls.serverEnters != 0 || calls.serverExits != 0 || calls.clientEnters != 0 || calls.clientExits != 0
        }
    }

    private fun binderServerFixture(): ByteArray {
        val writer = classWriter("example/ISync\$Stub", "android/os/Binder")
        writer.visitField(
            Opcodes.ACC_PRIVATE or Opcodes.ACC_STATIC or Opcodes.ACC_FINAL,
            "DESCRIPTOR",
            "Ljava/lang/String;",
            null,
            "example.ISync",
        )?.visitEnd()
        writer.method(
            Opcodes.ACC_PUBLIC,
            "onTransact",
            "(ILandroid/os/Parcel;Landroid/os/Parcel;I)Z",
        ) {
            visitInsn(Opcodes.ICONST_1)
            visitInsn(Opcodes.IRETURN)
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun binderProxyFixture(): ByteArray {
        val writer = classWriter("example/ISync\$Stub\$Proxy")
        writer.method(Opcodes.ACC_PUBLIC, "syncNow", "(Landroid/os/IBinder;)Z") {
            visitVarInsn(Opcodes.ALOAD, 1)
            visitInsn(Opcodes.ICONST_1)
            visitInsn(Opcodes.ACONST_NULL)
            visitInsn(Opcodes.ACONST_NULL)
            visitInsn(Opcodes.ICONST_1)
            visitMethodInsn(
                Opcodes.INVOKEINTERFACE,
                "android/os/IBinder",
                "transact",
                "(ILandroid/os/Parcel;Landroid/os/Parcel;I)Z",
                true,
            )
            visitInsn(Opcodes.IRETURN)
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun classWriter(name: String, parent: String = "java/lang/Object"): ClassWriter {
        return ClassWriter(ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS).apply {
            visit(Opcodes.V17, Opcodes.ACC_PUBLIC, name, null, parent, null)
        }
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

    private fun config(
        androidComponents: Boolean = false,
        binderIPC: Boolean = true,
    ): HookConfig = HookConfig(
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
        androidComponents = androidComponents,
        binderIPC = binderIPC,
    )

    private data class BinderCalls(
        var serverEnters: Int = 0,
        var serverExits: Int = 0,
        var clientEnters: Int = 0,
        var clientExits: Int = 0,
        var frameworkTransacts: Int = 0,
        val strings: MutableSet<String> = linkedSetOf(),
    )

    private companion object {
        const val JANK_HUNTER_ANDROID_HOOKS = "io/jankhunter/runtime/JankHunterAndroidHooks"
    }
}
