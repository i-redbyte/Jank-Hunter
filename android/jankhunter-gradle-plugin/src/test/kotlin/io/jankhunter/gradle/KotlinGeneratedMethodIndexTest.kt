package io.jankhunter.gradle

import kotlin.Metadata
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes

class KotlinGeneratedMethodIndexTest {
    @Test
    fun dataClassFunctionsAndDefaultAccessorAreProvenGenerated() {
        val index = index(GeneratedDataFixture::class.java)

        assertNull(index.reason("<init>", "(I)V"))
        assertEquals("kotlin_default_accessor", index.reason("getValue", "()I"))
        assertEquals("kotlin_generated_function", index.reason("component1", "()I"))
        assertEquals(
            "kotlin_generated_function",
            index.reason("equals", "(Ljava/lang/Object;)Z"),
        )
        assertEquals("kotlin_generated_function", index.reason("hashCode", "()I"))
        assertEquals("kotlin_generated_function", index.reason("toString", "()Ljava/lang/String;"))
    }

    @Test
    fun explicitObjectMethodsAndCustomAccessorsRemainApplicationCode() {
        val index = index(ExplicitMethodsFixture::class.java)

        assertNull(index.reason("<init>", "()V"))
        assertEquals("kotlin_default_accessor", index.reason("getGeneratedValue", "()I"))
        assertNull(index.reason("getCustomValue", "()I"))
        assertNull(index.reason("equals", "(Ljava/lang/Object;)Z"))
        assertNull(index.reason("hashCode", "()I"))
        assertNull(index.reason("toString", "()Ljava/lang/String;"))
        assertNull(index.reason("getManualValue", "()I"))
        assertNull(index.reason("syntheticButDeclared", "()I"))
    }

    @Test
    fun instrumentationUsesMetadataProofInsteadOfBroadMethodNames() {
        assertEquals(setOf("<init>"), instrumentedMethods(GeneratedDataFixture::class.java))
        assertEquals(
            setOf(
                "<init>",
                "getCustomValue",
                "getManualValue",
                "syntheticButDeclared",
                "equals",
                "hashCode",
                "toString",
            ),
            instrumentedMethods(ExplicitMethodsFixture::class.java),
        )
    }

    @Test
    fun metadataAbsenceDoesNotSilentlyExcludeObjectConstructor() {
        val index = index(GeneratedObjectFixture::class.java)

        assertEquals(KotlinMethodOrigin.UNKNOWN, index.origin("<init>", "()V"))
        assertEquals(setOf("<init>"), instrumentedMethods(GeneratedObjectFixture::class.java))
    }

    @Test
    fun localFunctionMissingFromMetadataRemainsUnknown() {
        val index = index(LocalFunctionFixture::class.java)
        val localMethod = bytecodeMethods(LocalFunctionFixture::class.java)
            .single { method -> method.first.contains("\$factorial") }

        assertEquals(KotlinMethodOrigin.UNKNOWN, index.origin(localMethod.first, localMethod.second))
        assertTrue(localMethod.first in instrumentedMethods(LocalFunctionFixture::class.java))
    }

    @Test
    fun declaredEnumConstructorAndMethodsRemainApplicationCode() {
        val index = index(DeclaredEnumFixture::class.java)
        val constructor = DeclaredEnumFixture::class.java.declaredConstructors.single()
        val descriptor = org.objectweb.asm.Type.getConstructorDescriptor(constructor)

        assertNull(index.reason("<init>", descriptor))
        assertEquals(setOf("<init>", "calculate"), instrumentedMethods(DeclaredEnumFixture::class.java))
    }

    private fun index(type: Class<*>): KotlinGeneratedMethodIndex {
        val metadata = type.getAnnotation(Metadata::class.java)
        assertNotNull(metadata)
        return KotlinGeneratedMethodIndex.from(metadata)
    }

    private fun instrumentedMethods(type: Class<*>): Set<String> {
        val resource = "/${type.name.replace('.', '/')}.class"
        val source = type.getResourceAsStream(resource)?.use { it.readBytes() }
            ?: error("Missing class resource $resource")
        val reader = ClassReader(source)
        val writer = ClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        reader.accept(
            JankHunterClassVisitor(
                writer,
                reader.className,
                HookConfig(
                    methodCounters = false,
                    okhttp = false,
                    webSockets = false,
                    handlers = false,
                    executors = false,
                    coroutines = false,
                    flowInteractions = false,
                    logSpam = false,
                    classGraph = false,
                    runtimeCallGraph = true,
                    classGraphDirectory = "",
                    instrumentationDiagnosticsDirectory = "",
                    ownerMapEntriesDirectory = "",
                ),
            ),
            ClassReader.EXPAND_FRAMES,
        )
        val result = linkedSetOf<String>()
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
                        override fun visitMethodInsn(
                            opcodeAndSource: Int,
                            owner: String,
                            methodName: String,
                            descriptor: String,
                            isInterface: Boolean,
                        ) {
                            if (owner == JANK_HUNTER_HOOKS && methodName == "enterMethod") result += name
                        }
                    }
                }
            },
            0,
        )
        return result
    }

    private fun bytecodeMethods(type: Class<*>): List<Pair<String, String>> {
        val resource = "/${type.name.replace('.', '/')}.class"
        val source = type.getResourceAsStream(resource)?.use { it.readBytes() }
            ?: error("Missing class resource $resource")
        val result = arrayListOf<Pair<String, String>>()
        ClassReader(source).accept(
            object : ClassVisitor(Opcodes.ASM9) {
                override fun visitMethod(
                    access: Int,
                    name: String,
                    descriptor: String,
                    signature: String?,
                    exceptions: Array<out String>?,
                ): MethodVisitor {
                    result += name to descriptor
                    return object : MethodVisitor(Opcodes.ASM9) {}
                }
            },
            0,
        )
        return result
    }

    private companion object {
        const val JANK_HUNTER_HOOKS = "io/jankhunter/runtime/JankHunterHooks"
    }
}

private data class GeneratedDataFixture(val value: Int)

private object GeneratedObjectFixture

private class LocalFunctionFixture {
    fun calculate(value: Int): Int {
        fun factorial(input: Int): Int = if (input <= 1) 1 else input * factorial(input - 1)
        return factorial(value)
    }
}

private enum class DeclaredEnumFixture(private val weight: Int) {
    VALUE(3),
    ;

    fun calculate(): Int = weight * 2
}

private class ExplicitMethodsFixture {
    val generatedValue = 1
    val customValue: Int
        get() = generatedValue + 1

    fun getManualValue(): Int = generatedValue

    @JvmSynthetic
    fun syntheticButDeclared(): Int = generatedValue

    override fun equals(other: Any?): Boolean = other === this

    override fun hashCode(): Int = 32597

    override fun toString(): String = "explicit"
}
