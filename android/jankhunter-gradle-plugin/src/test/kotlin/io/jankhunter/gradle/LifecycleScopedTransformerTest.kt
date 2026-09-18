package io.jankhunter.gradle

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Label
import org.objectweb.asm.Opcodes
import org.objectweb.asm.tree.ClassNode
import org.objectweb.asm.tree.MethodInsnNode

class LifecycleScopedTransformerTest {
    @Test
    fun optimizedCustomDelegateIsDiagnosedWithoutCallingItsGetter() {
        val fixture = OptimizedDelegateFixture::class.java
        val original = ClassNode().also { node ->
            fixture.getResourceAsStream("/${fixture.name.replace('.', '/')}.class")!!.use {
                ClassReader(it).accept(node, 0)
            }
        }
        org.junit.Assert.assertFalse(original.fields.any { it.name == "custom\$delegate" })
        val node = ClassNode().also { ClassReader(model("app/Fragment", "androidx/fragment/app/Fragment")).accept(it, 0) }
        for (callback in listOf("onDestroyView", "onDestroy")) {
            node.visitMethod(Opcodes.ACC_PUBLIC, callback, "()V", null, null).apply {
                visitCode(); visitInsn(Opcodes.RETURN); visitMaxs(0, 1); visitEnd()
            }
        }
        node.visibleAnnotations = original.visibleAnnotations
        val source = ClassWriter(0).also { node.accept(it) }.toByteArray()
        val index = LifecycleClassIndex().apply {
            add(model("androidx/fragment/app/Fragment", "java/lang/Object"), false)
            add(source, true)
        }
        val diagnostics = arrayListOf<String>()
        LifecycleScopedTransformer(index, setOf("app/Fragment"), "", diagnostic = diagnostics::add).transform(source)
        assertEquals(1, diagnostics.size)
        org.junit.Assert.assertTrue(diagnostics.single().contains("custom"))
    }

    @Test
    fun inheritedAccessorNameCollisionFailsBeforeAnyClassIsEmitted() {
        val parent = ClassNode().also { ClassReader(model("app/Base", "androidx/lifecycle/ViewModel")).accept(it, 0) }
        parent.visitMethod(Opcodes.ACC_PUBLIC or Opcodes.ACC_FINAL, LifecycleAccessorEmitter.KIND_METHOD, "()I", null, null).apply {
            visitCode(); visitInsn(Opcodes.ICONST_0); visitInsn(Opcodes.IRETURN); visitMaxs(1, 1); visitEnd()
        }
        val source = model("app/Child", "app/Base")
        val index = LifecycleClassIndex().apply {
            add(model("androidx/lifecycle/ViewModel", "java/lang/Object"), false)
            add(ClassWriter(0).also { parent.accept(it) }.toByteArray(), true)
            add(source, true)
        }
        assertThrows(IllegalStateException::class.java) {
            LifecycleScopedTransformer(index, setOf("app/Child"), "") {}.transform(source)
        }
    }

    @Test
    fun annotatedCallbackHasVerifierFramesForInjectedExceptionHandler() {
        val root = model("androidx/lifecycle/ViewModel", "java/lang/Object")
        val sourceNode = ClassNode().also { ClassReader(model("app/Annotated", "androidx/lifecycle/ViewModel")).accept(it, 0) }
        sourceNode.visitAnnotation("Lio/jankhunter/annotations/JankHunterOwner;", false).apply {
            visit("value", "probe.owner")
            visitEnd()
        }
        val source = ClassWriter(0).also { sourceNode.accept(it) }.toByteArray()
        val index = LifecycleClassIndex().apply { add(root, false); add(source, true) }
        val result = LifecycleScopedTransformer(index, setOf("app/Annotated"), "") {}.transform(source)
        val loader = object : ClassLoader(javaClass.classLoader) {
            fun define(bytes: ByteArray): Class<*> = defineClass(null, bytes, 0, bytes.size)
        }
        loader.define(accessorInterface(LifecycleAccessorEmitter.SINK, "accept", "(Ljava/lang/Object;Ljava/lang/String;)V"))
        loader.define(accessorInterface(LifecycleAccessorEmitter.ACCESSOR, LifecycleAccessorEmitter.VISIT_METHOD, LifecycleAccessorEmitter.VISIT_DESCRIPTOR))
        loader.define(root)
        loader.define(result).declaredMethods
    }

    private fun accessorInterface(name: String, method: String, descriptor: String): ByteArray {
        val writer = ClassWriter(0)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC or Opcodes.ACC_ABSTRACT or Opcodes.ACC_INTERFACE, name, null, "java/lang/Object", null)
        writer.visitMethod(Opcodes.ACC_PUBLIC or Opcodes.ACC_ABSTRACT, method, descriptor, null, null).visitEnd()
        writer.visitEnd()
        return writer.toByteArray()
    }

    @Test
    fun callbackWithCompressedFramesTransformsAndSecondPassIsIdentical() {
        val source = model("app/Model", "androidx/lifecycle/ViewModel", branch = true)
        val index = LifecycleClassIndex().apply {
            add(model("androidx/lifecycle/ViewModel", "java/lang/Object"), false)
            add(source, true)
        }
        val transform = LifecycleScopedTransformer(index, setOf("app/Model"), "") {}
        val result = transform.transform(source)
        val node = ClassNode().also { ClassReader(result).accept(it, 0) }
        val calls = node.methods.single { it.name == "onCleared" }.instructions.asSequence().filterIsInstance<MethodInsnNode>()
        assertEquals(1, calls.count { it.name == "watchLifecycleObject" })
        assertArrayEquals(result, transform.transform(result))
    }

    private fun model(name: String, parent: String, branch: Boolean = false): ByteArray {
        val writer = ClassWriter(ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, name, null, parent, null)
        writer.visitMethod(Opcodes.ACC_PROTECTED, "onCleared", "()V", null, null).apply {
            visitCode()
            if (branch) {
                val finish = Label()
                visitVarInsn(Opcodes.ALOAD, 0)
                visitJumpInsn(Opcodes.IFNONNULL, finish)
                visitInsn(Opcodes.NOP)
                visitLabel(finish)
            }
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }
}

private class OptimizedDelegateFixture {
    private val existing = kotlin.properties.ReadOnlyProperty<Any?, Any> { _, _ -> error("Must not invoke") }
    val custom by existing
    val initializedOnly by lazy { Any() }
}
