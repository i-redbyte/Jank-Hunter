package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Opcodes

class JavaKotlinMixedMetadataTest {
    @Test
    fun javaClassStaysAbsentWhileKotlinClassDecodesInSameSession() {
        val javaBytes = ClassWriter(0).apply {
            visit(Opcodes.V11, Opcodes.ACC_PUBLIC, "fixture/MixedJava", null, "java/lang/Object", null)
            visitMethod(Opcodes.ACC_PUBLIC, "run", "()V", null, null).visitEnd()
            visitEnd()
        }.toByteArray()
        val kotlinBytes = MixedKotlinProbe::class.java.getResourceAsStream(
            "/${MixedKotlinProbe::class.java.name.replace('.', '/')}.class",
        )!!.use { it.readBytes() }

        assertEquals(KotlinMetadataAnalysisStatus.ABSENT, index(javaBytes).analysisStatus)
        assertEquals(KotlinMetadataAnalysisStatus.READABLE, index(kotlinBytes).analysisStatus)
    }

    private fun index(bytes: ByteArray): KotlinGeneratedMethodIndex {
        var index = KotlinGeneratedMethodIndex.EMPTY
        ClassReader(bytes).accept(object : org.objectweb.asm.ClassVisitor(Opcodes.ASM9) {
            override fun visitAnnotation(descriptor: String, visible: Boolean): org.objectweb.asm.AnnotationVisitor? {
                if (descriptor != KotlinGeneratedMethodIndex.METADATA_DESCRIPTOR) return null
                return KotlinGeneratedMethodIndex.collectingVisitor(null) { index = it }
            }
        }, ClassReader.SKIP_CODE)
        return index
    }
}

private data class MixedKotlinProbe(val value: Int)
