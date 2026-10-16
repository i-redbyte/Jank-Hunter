package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Opcodes
import org.objectweb.asm.tree.ClassNode
import org.objectweb.asm.tree.MethodInsnNode

class OkHttpDispatchInstrumentationTest {
    @Test
    fun customAndNullInvocationsBypassTheNonNullKotlinHelper() {
        val loader = object : ClassLoader(javaClass.classLoader) {
            fun define(bytes: ByteArray): Class<*> = defineClass(null, bytes, 0, bytes.size)
        }
        fun fixture(name: String, parent: String = "java/lang/Object", newCall: Boolean = false): ByteArray {
            val writer = ClassWriter(0)
            writer.visit(Opcodes.V1_8, Opcodes.ACC_PUBLIC, name, null, parent, null)
            writer.visitMethod(Opcodes.ACC_PUBLIC, "<init>", "()V", null, null).apply {
                visitCode(); visitVarInsn(Opcodes.ALOAD, 0)
                visitMethodInsn(Opcodes.INVOKESPECIAL, parent, "<init>", "()V", false)
                visitInsn(Opcodes.RETURN); visitMaxs(1, 1); visitEnd()
            }
            if (newCall) writer.visitMethod(Opcodes.ACC_PUBLIC, "newCall", "(Lokhttp3/Request;)Lokhttp3/Call;", null, null).apply {
                visitCode(); visitInsn(Opcodes.ACONST_NULL); visitInsn(Opcodes.ARETURN); visitMaxs(1, 2); visitEnd()
            }
            writer.visitEnd()
            return writer.toByteArray()
        }
        val request = loader.define(fixture("okhttp3/Request"))
        loader.define(fixture("okhttp3/Call"))
        val base = loader.define(fixture("okhttp3/OkHttpClient", newCall = true))
        val custom = loader.define(fixture("example/NullableClient", "okhttp3/OkHttpClient", true))
        val helper = ClassWriter(0)
        helper.visit(Opcodes.V1_8, Opcodes.ACC_PUBLIC, "io/jankhunter/okhttp3/JankHunterOkHttp3", null, "java/lang/Object", null)
        helper.visitMethod(Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC, "newCall",
            "(Lokhttp3/OkHttpClient;Lokhttp3/Request;)Lokhttp3/Call;", null, null).apply {
            visitCode(); visitTypeInsn(Opcodes.NEW, "java/lang/AssertionError"); visitInsn(Opcodes.DUP)
            visitLdcInsn("Original invocation must bypass the non-null helper")
            visitMethodInsn(Opcodes.INVOKESPECIAL, "java/lang/AssertionError", "<init>", "(Ljava/lang/Object;)V", false)
            visitInsn(Opcodes.ATHROW); visitMaxs(3, 2); visitEnd()
        }
        helper.visitEnd(); loader.define(helper.toByteArray())
        val caller = ClassWriter(0)
        caller.visit(Opcodes.V1_8, Opcodes.ACC_PUBLIC, "example/CallSite", null, "java/lang/Object", null)
        caller.visitMethod(Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC, "invoke",
            "(Lokhttp3/OkHttpClient;Lokhttp3/Request;)Lokhttp3/Call;", null, null).apply {
            visitCode(); visitVarInsn(Opcodes.ALOAD, 0); visitVarInsn(Opcodes.ALOAD, 1)
            visitMethodInsn(Opcodes.INVOKEVIRTUAL, "okhttp3/OkHttpClient", "newCall", "(Lokhttp3/Request;)Lokhttp3/Call;", false)
            visitInsn(Opcodes.ARETURN); visitMaxs(2, 2); visitEnd()
        }
        caller.visitEnd()
        val reader = ClassReader(caller.toByteArray())
        val output = ClassWriter(ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        reader.accept(JankHunterClassVisitor(output, reader.className, configuration()), ClassReader.EXPAND_FRAMES)
        val invoke = loader.define(output.toByteArray()).getMethod("invoke", base, request)
        org.junit.Assert.assertNull(invoke.invoke(null, custom.getConstructor().newInstance(), request.getConstructor().newInstance()))
        org.junit.Assert.assertNull(invoke.invoke(null, base.getConstructor().newInstance(), null))
        val failure = org.junit.Assert.assertThrows(java.lang.reflect.InvocationTargetException::class.java) {
            invoke.invoke(null, null, request.getConstructor().newInstance())
        }
        assertTrue(failure.cause is NullPointerException)
    }

    @Test
    fun superNewCallRemainsAnInvokeSpecialInsteadOfRedispatchingToTheOverride() {
        val calls = instrument(Opcodes.INVOKESPECIAL)
        assertTrue(calls.any { it.opcode == Opcodes.INVOKESPECIAL && it.owner == "okhttp3/OkHttpClient" && it.name == "newCall" })
        assertTrue(calls.none { it.owner == "io/jankhunter/okhttp3/JankHunterOkHttp3" && it.name == "newCall" })
    }

    @Test
    fun virtualNewCallStillUsesTheCompatibilityGuard() {
        val calls = instrument(Opcodes.INVOKEVIRTUAL)
        assertEquals(1, calls.count { it.owner == "io/jankhunter/okhttp3/JankHunterOkHttp3" && it.name == "newCall" })
    }

    private fun instrument(opcode: Int): List<MethodInsnNode> {
        val descriptor = "(Lokhttp3/Request;)Lokhttp3/Call;"
        val fixture = ClassWriter(0)
        fixture.visit(Opcodes.V1_8, Opcodes.ACC_PUBLIC, "example/CustomClient", null, "okhttp3/OkHttpClient", null)
        fixture.visitMethod(Opcodes.ACC_PUBLIC, "newCall", descriptor, null, null).apply {
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            visitVarInsn(Opcodes.ALOAD, 1)
            visitMethodInsn(opcode, "okhttp3/OkHttpClient", "newCall", descriptor, false)
            visitInsn(Opcodes.ARETURN)
            visitMaxs(2, 2)
            visitEnd()
        }
        fixture.visitEnd()
        val reader = ClassReader(fixture.toByteArray())
        val output = ClassWriter(ClassWriter.COMPUTE_MAXS)
        reader.accept(JankHunterClassVisitor(output, reader.className,
            configuration()), ClassReader.EXPAND_FRAMES)
        val node = ClassNode()
        ClassReader(output.toByteArray()).accept(node, 0)
        return node.methods.single { it.name == "newCall" }.instructions.filterIsInstance<MethodInsnNode>()
    }

    private fun configuration() = HookConfig(
        methodCounters = false, okhttp = true, webSockets = false, handlers = false,
        executors = false, coroutines = false, interactionOperations = false, logSpam = false,
        classGraph = false, runtimeCallGraph = false, classGraphDirectory = "",
        instrumentationDiagnosticsDirectory = "", okHttpHelperAvailable = true,
    )
}
