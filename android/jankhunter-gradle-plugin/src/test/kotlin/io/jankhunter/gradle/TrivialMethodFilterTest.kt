package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.objectweb.asm.Opcodes
import org.objectweb.asm.tree.MethodNode

class TrivialMethodFilterTest {
    @Test
    fun bodyProofExcludesPlainFieldAccessRegardlessOfName() {
        val getter = method("load", "()I") {
            visitVarInsn(Opcodes.ALOAD, 0)
            visitFieldInsn(Opcodes.GETFIELD, "test/Owner", "value", "I")
            visitInsn(Opcodes.IRETURN)
        }
        assertEquals("trivial_field_access", decision(getter))
        val setter = method("save", "(I)V") {
            visitVarInsn(Opcodes.ALOAD, 0)
            visitVarInsn(Opcodes.ILOAD, 1)
            visitFieldInsn(Opcodes.PUTFIELD, "test/Owner", "value", "I")
            visitInsn(Opcodes.RETURN)
        }
        assertEquals("trivial_field_access", decision(setter))
    }

    @Test
    fun namesAndSmallSizeDoNotExcludeCallsSynchronizationOrUnknownFields() {
        val call = method("getValue", "()I") {
            visitVarInsn(Opcodes.ALOAD, 0)
            visitMethodInsn(Opcodes.INVOKEVIRTUAL, "test/Owner", "compute", "()I", false)
            visitInsn(Opcodes.IRETURN)
        }
        assertNull(decision(call))
        val getter = method("getValue", "()I") {
            visitVarInsn(Opcodes.ALOAD, 0)
            visitFieldInsn(Opcodes.GETFIELD, "test/Owner", "value", "I")
            visitInsn(Opcodes.IRETURN)
        }
        assertNull(TrivialMethodClassifier.classify(getter, "test/Owner", emptyMap()))
        getter.access = getter.access or Opcodes.ACC_SYNCHRONIZED
        assertNull(decision(getter))
    }

    @Test
    fun constantReturnAndEmptyBodyAreTrivialButConstructorsAreKept() {
        assertEquals("trivial_constant_return", decision(method("ready", "()Z") {
            visitInsn(Opcodes.ICONST_1)
            visitInsn(Opcodes.IRETURN)
        }))
        assertEquals("trivial_empty_body", decision(method("noop", "()V") { visitInsn(Opcodes.RETURN) }))
        assertNull(decision(method("<init>", "()V") { visitInsn(Opcodes.RETURN) }))
    }

    private fun decision(method: MethodNode): String? =
        TrivialMethodClassifier.classify(method, "test/Owner", mapOf("value" to "I"))?.exclusionReason

    private fun method(name: String, descriptor: String, body: MethodNode.() -> Unit): MethodNode =
        MethodNode(Opcodes.ASM9, Opcodes.ACC_PUBLIC, name, descriptor, null, null).apply(body)
}
