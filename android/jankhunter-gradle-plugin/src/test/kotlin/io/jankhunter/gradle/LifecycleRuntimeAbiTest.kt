package io.jankhunter.gradle

import org.junit.Assert.assertThrows
import org.junit.Test
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Opcodes

class LifecycleRuntimeAbiTest {
    @Test fun completeAbiIsAccepted() = LifecycleRuntimeAbi.validate(index())

    @Test fun missingTypedHookIsRejected() {
        assertThrows(IllegalStateException::class.java) { LifecycleRuntimeAbi.validate(index(typedHook = false)) }
    }

    @Test fun missingLegacyHookIsRejected() {
        assertThrows(IllegalStateException::class.java) { LifecycleRuntimeAbi.validate(index(legacyHook = false)) }
    }

    @Test fun nonPublicSinkIsRejected() {
        assertThrows(IllegalStateException::class.java) { LifecycleRuntimeAbi.validate(index(publicSink = false)) }
    }

    @Test fun staticInterfaceMethodIsRejected() {
        assertThrows(IllegalStateException::class.java) { LifecycleRuntimeAbi.validate(index(staticSink = true)) }
    }

    private fun index(
        typedHook: Boolean = true,
        legacyHook: Boolean = true,
        publicSink: Boolean = true,
        staticSink: Boolean = false,
    ): LifecycleClassIndex {
        val index = LifecycleClassIndex()
        fun add(name: String, flags: Int, methods: List<Triple<String, String, Int>>) {
            val writer = ClassWriter(0)
            writer.visit(Opcodes.V17, flags, name, null, "java/lang/Object", null)
            methods.forEach { (method, descriptor, access) -> writer.visitMethod(access, method, descriptor, null, null).visitEnd() }
            writer.visitEnd()
            index.add(writer.toByteArray(), false)
        }
        val abstractPublic = Opcodes.ACC_PUBLIC or Opcodes.ACC_ABSTRACT
        val interfaceFlags = abstractPublic or Opcodes.ACC_INTERFACE
        add(LifecycleAccessorEmitter.ACCESSOR, interfaceFlags, listOf(
            Triple(LifecycleAccessorEmitter.KIND_METHOD, "()I", abstractPublic),
            Triple(LifecycleAccessorEmitter.VISIT_METHOD, LifecycleAccessorEmitter.VISIT_DESCRIPTOR, abstractPublic),
        ))
        add(LifecycleAccessorEmitter.SINK, if (publicSink) interfaceFlags else interfaceFlags and Opcodes.ACC_PUBLIC.inv(), listOf(
            Triple("accept", "(Ljava/lang/Object;Ljava/lang/String;)V", if (staticSink) Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC else abstractPublic),
        ))
        val hooks = arrayListOf<Triple<String, String, Int>>()
        if (typedHook) hooks.add(Triple("watchLifecycleObject", "(Ljava/lang/Object;ILjava/lang/String;Ljava/lang/String;)V", Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC))
        if (legacyHook) hooks.add(Triple("watchLifecycleObject", "(Ljava/lang/Object;Ljava/lang/String;Ljava/lang/String;)V", Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC))
        add("io/jankhunter/runtime/JankHunterHooks", Opcodes.ACC_PUBLIC, hooks)
        return index
    }
}
