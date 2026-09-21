package io.jankhunter.gradle

import java.lang.reflect.Proxy
import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Opcodes
import org.objectweb.asm.tree.ClassNode

class LifecycleAccessorEmitterTest {
    @Test
    fun nonFragmentAccessorDoesNotReadOrVisitViewFields() {
        val loader = FixtureLoader(javaClass.classLoader)
        val sinkClass = loader.define(interfaceType(LifecycleAccessorEmitter.SINK, "accept", "(Ljava/lang/Object;Ljava/lang/String;)V"))
        loader.define(interfaceType(LifecycleAccessorEmitter.ACCESSOR, LifecycleAccessorEmitter.VISIT_METHOD, LifecycleAccessorEmitter.VISIT_DESCRIPTOR))
        val viewClass = loader.define(plainType("android/view/View"))
        val candidate = node(plainType("fixture/Model", fields = mapOf("view" to "Ljava/lang/Object;")))
        LifecycleAccessorEmitter(candidate, LifecycleTargetKind.VIEW_MODEL, false, false, false).emit()
        val candidateClass = loader.define(bytes(candidate))
        val instance = candidateClass.getConstructor().newInstance()
        set(instance, "view", viewClass.getConstructor().newInstance())
        val captured = ArrayList<Any?>()
        val sink = Proxy.newProxyInstance(loader, arrayOf(sinkClass)) { _, _, args -> captured.add(args[0]); null }
        candidateClass.getMethod(LifecycleAccessorEmitter.VISIT_METHOD, sinkClass, String::class.java).invoke(instance, sink, "owner")
        assertEquals(emptyList<Any>(), captured)
    }

    @Test
    fun verifierExecutesPrivateErasedBindingCaptureWithoutInitializingLazyOrMatchingNames() {
        val loader = FixtureLoader(javaClass.classLoader)
        val sinkClass = loader.define(interfaceType(LifecycleAccessorEmitter.SINK, "accept", "(Ljava/lang/Object;Ljava/lang/String;)V"))
        loader.define(interfaceType(LifecycleAccessorEmitter.ACCESSOR, LifecycleAccessorEmitter.VISIT_METHOD, LifecycleAccessorEmitter.VISIT_DESCRIPTOR))
        val viewClass = loader.define(plainType("android/view/View"))
        loader.define(interfaceType(LifecycleAccessorEmitter.BINDING, "getRoot", "()Landroid/view/View;"))
        val bindingNode = node(plainType("fixture/RealBinding", listOf(LifecycleAccessorEmitter.BINDING), mapOf("root" to "Landroid/view/View;")))
        bindingNode.visitMethod(Opcodes.ACC_PUBLIC, "getRoot", "()Landroid/view/View;", null, null).apply {
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            visitFieldInsn(Opcodes.GETFIELD, bindingNode.name, "root", "Landroid/view/View;")
            visitInsn(Opcodes.ARETURN)
            visitMaxs(1, 1)
            visitEnd()
        }
        val bindingClass = loader.define(bytes(bindingNode))
        val candidate = node(plainType("fixture/Candidate", fields = mapOf(
            "binding" to "Ljava/lang/Object;", "fake" to "Ljava/lang/Object;", "pending" to "Lkotlin/Lazy;",
        )))
        LifecycleAccessorEmitter(candidate, LifecycleTargetKind.FRAGMENT, false, true, true).emit()
        val candidateClass = loader.define(bytes(candidate))
        val root = viewClass.getConstructor().newInstance()
        val binding = bindingClass.getConstructor().newInstance()
        set(binding, "root", root)
        val instance = candidateClass.getConstructor().newInstance()
        set(instance, "binding", binding)
        set(instance, "fake", FakeBinding())
        var initializations = 0
        set(instance, "pending", lazy { initializations++; binding })
        val captured = ArrayList<Any?>()
        val sink = Proxy.newProxyInstance(loader, arrayOf(sinkClass)) { _, _, args ->
            captured.add(args[0])
            null
        }
        candidateClass.getMethod(LifecycleAccessorEmitter.VISIT_METHOD, sinkClass, String::class.java)
            .invoke(instance, sink, "owner")
        assertEquals(0, initializations)
        assertEquals(2, captured.size)
        assertSame(binding, captured[0])
        assertSame(root, captured[1])
    }

    private fun set(instance: Any, name: String, value: Any) {
        instance.javaClass.getDeclaredField(name).apply { isAccessible = true }.set(instance, value)
    }

    private fun interfaceType(name: String, method: String, descriptor: String): ByteArray {
        val writer = ClassWriter(0)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC or Opcodes.ACC_ABSTRACT or Opcodes.ACC_INTERFACE, name, null, "java/lang/Object", null)
        writer.visitMethod(Opcodes.ACC_PUBLIC or Opcodes.ACC_ABSTRACT, method, descriptor, null, null).visitEnd()
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun plainType(name: String, interfaces: List<String> = emptyList(), fields: Map<String, String> = emptyMap()): ByteArray {
        val writer = ClassWriter(0)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, name, null, "java/lang/Object", interfaces.toTypedArray())
        fields.forEach { (field, descriptor) -> writer.visitField(Opcodes.ACC_PRIVATE, field, descriptor, null, null).visitEnd() }
        writer.visitMethod(Opcodes.ACC_PUBLIC, "<init>", "()V", null, null).apply {
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            visitMethodInsn(Opcodes.INVOKESPECIAL, "java/lang/Object", "<init>", "()V", false)
            visitInsn(Opcodes.RETURN)
            visitMaxs(1, 1)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun node(bytes: ByteArray): ClassNode = ClassNode().also { ClassReader(bytes).accept(it, 0) }
    private fun bytes(node: ClassNode): ByteArray = ClassWriter(0).also { node.accept(it) }.toByteArray()
    private class FakeBinding
    private class FixtureLoader(parent: ClassLoader) : ClassLoader(parent) {
        fun define(bytes: ByteArray): Class<*> = defineClass(null, bytes, 0, bytes.size)
    }
}
