package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Opcodes

class LifecycleFrameTypesTest {
    @Test
    fun mergesArrayDimensionsPrimitiveArraysAndInterfaceCovariance() {
        val index = LifecycleClassIndex()
        add(index, "java/lang/Object", null)
        add(index, "sample/Marker", "java/lang/Object", isInterface = true)
        add(index, "sample/Base", "java/lang/Object")
        add(index, "sample/A", "sample/Base", interfaces = arrayOf("sample/Marker"))
        add(index, "sample/B", "sample/Base")
        val types = LifecycleFrameTypes(index)
        assertEquals("sample/Base", types.common("sample/A", "sample/B"))
        assertEquals("sample/Marker", types.common("sample/Marker", "sample/A"))
        assertEquals("[Lsample/Base;", types.common("[Lsample/A;", "[Lsample/B;"))
        assertEquals("[Ljava/lang/Object;", types.common("[[Lsample/A;", "[Lsample/B;"))
        assertEquals("[Ljava/lang/Object;", types.common("[[I", "[[J"))
        assertEquals("java/lang/Object", types.common("[I", "[J"))
        assertEquals("java/lang/Cloneable", types.common("[I", "java/lang/Cloneable"))
        assertEquals("[Ljava/lang/Cloneable;", types.common("[[I", "[Ljava/lang/Cloneable;"))
    }

    @Test
    fun missingMetadataFailsInsteadOfGuessingAVerificationType() {
        val index = LifecycleClassIndex()
        add(index, "sample/A", "missing/Parent")
        assertThrows(IllegalStateException::class.java) { LifecycleFrameTypes(index).common("sample/A", "missing/B") }
    }

    private fun add(index: LifecycleClassIndex, name: String, parent: String?, interfaces: Array<String>? = null, isInterface: Boolean = false) {
        val writer = ClassWriter(0)
        val flags = if (isInterface) Opcodes.ACC_INTERFACE or Opcodes.ACC_ABSTRACT else 0
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC or flags, name, null, parent, interfaces)
        writer.visitEnd()
        index.add(writer.toByteArray(), false)
    }
}
