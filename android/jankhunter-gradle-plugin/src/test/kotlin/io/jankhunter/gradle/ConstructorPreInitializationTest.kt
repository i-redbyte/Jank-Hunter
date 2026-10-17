package io.jankhunter.gradle

import java.lang.reflect.InvocationTargetException
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Label
import org.objectweb.asm.Opcodes
import org.objectweb.asm.tree.ClassNode
import org.objectweb.asm.tree.MethodInsnNode

class ConstructorPreInitializationTest {
    @Test
    fun validNearLimitMethodRemainsExecutableWithoutOptionalHooks() {
        val original = ClassWriter(ClassWriter.COMPUTE_MAXS).apply {
            visit(Opcodes.V1_8, Opcodes.ACC_PUBLIC, "fixture/LargeMethod", null, "java/lang/Object", null)
            visitMethod(Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC, "work", "()V", null, null).apply {
                visitCode()
                visitMethodInsn(Opcodes.INVOKESTATIC, "java/lang/System", "nanoTime", "()J", false)
                visitInsn(Opcodes.POP2)
                repeat(65_520) { visitInsn(Opcodes.NOP) }
                visitInsn(Opcodes.RETURN)
                visitMaxs(0, 0)
                visitEnd()
            }
            visitEnd()
        }.toByteArray()
        for (database in listOf(false, true)) {
            for (mode in JankHunterMethodFilterMode.entries) {
                val bytes = transform(original, database, mode)
                val node = ClassNode().also { ClassReader(bytes).accept(it, 0) }
                assertTrue(node.methods.single().instructions.toArray().filterIsInstance<MethodInsnNode>()
                    .none { it.owner == "io/jankhunter/runtime/JankHunterHooks" })
                val loader = object : ClassLoader(javaClass.classLoader) {
                    fun define(): Class<*> = defineClass(null, bytes, 0, bytes.size)
                }
                loader.define().getMethod("work").invoke(null)
            }
        }
    }

    @Test
    fun aHandlerForAnArgumentFailureBeforeSuperRemainsVerifiableAfterInstrumentation() {
        val original = original()
        checkConstructor(original)
        for (database in listOf(false, true)) {
            for (mode in JankHunterMethodFilterMode.entries) checkConstructor(transform(original, database, mode, saveFixture = true))
        }
    }

    @Test
    fun aRealKotlinSuperArgumentTryExpressionRemainsExecutable() {
        val name = KotlinPreInitialization::class.java.name.replace('.', '/') + ".class"
        val original = checkNotNull(javaClass.classLoader.getResourceAsStream(name)).use { it.readBytes() }
        checkConstructor(original)
        checkConstructor(transform(original))
    }

    @Test
    fun anOrdinaryPostSuperHandlerStillReceivesGraphHooks() {
        val transformed = transform(original(beforeSuper = false, extraAllocation = true))
        val node = ClassNode().also { ClassReader(transformed).accept(it, 0) }
        assertTrue(node.methods.single().instructions.toArray().filterIsInstance<MethodInsnNode>()
            .any { it.owner == "io/jankhunter/runtime/JankHunterHooks" && it.name == "enterMethod" })
        checkConstructor(transformed)
    }

    @Test
    fun aConstructorWithoutHandlerFramesRemainsIntactInsteadOfGuessingInitialization() {
        val transformed = transform(original(), readerFlags = ClassReader.SKIP_FRAMES)
        val node = ClassNode().also { ClassReader(transformed).accept(it, 0) }
        assertTrue(node.methods.single().instructions.toArray().filterIsInstance<MethodInsnNode>()
            .none { it.owner == "io/jankhunter/runtime/JankHunterHooks" })
        checkConstructor(transformed)
    }

    @Test
    fun alternativeSuperCallBranchesRemainVerifiableAndExecutable() {
        val original = branchingOriginal()
        checkConstructor(original)
        for (database in listOf(false, true)) {
            for (mode in JankHunterMethodFilterMode.entries) {
                val transformed = transform(original, database, mode)
                saveFixture(original, transformed, "jh.constructor.branch.fixture.output")
                checkConstructor(transformed)
            }
        }
    }

    @Test
    fun aLatePhysicalPrologueWithOneSuperCallRemainsVerifiable() {
        val original = branchingOriginal(latePrologue = true)
        checkConstructor(original)
        val transformed = transform(original)
        saveFixture(original, transformed, "jh.constructor.prologue.fixture.output")
        checkConstructor(transformed)
    }

    @Test
    fun aRealKotlinConditionalArgumentStillReceivesHooks() {
        val name = KotlinConditionalArgument::class.java.name.replace('.', '/') + ".class"
        val original = checkNotNull(javaClass.classLoader.getResourceAsStream(name)).use { it.readBytes() }
        val transformed = transform(original)
        val node = ClassNode().also { ClassReader(transformed).accept(it, 0) }
        assertTrue(node.methods.single().instructions.toArray().filterIsInstance<MethodInsnNode>()
            .any { it.owner == "io/jankhunter/runtime/JankHunterHooks" && it.name == "enterMethod" })
        checkConstructor(transformed)
    }

    private fun transform(original: ByteArray, database: Boolean = true,
        mode: JankHunterMethodFilterMode = JankHunterMethodFilterMode.FILTER, saveFixture: Boolean = false,
        readerFlags: Int = ClassReader.EXPAND_FRAMES): ByteArray {
        val reader = ClassReader(original)
        val writer = ClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        val config = HookConfig(methodCounters = false, okhttp = false, webSockets = false, handlers = false,
            executors = false, coroutines = false, interactionOperations = false, logSpam = false,
            classGraph = false, runtimeCallGraph = true, databaseTracing = database, methodFilterMode = mode,
            classGraphDirectory = "", instrumentationDiagnosticsDirectory = "")
        reader.accept(JankHunterClassVisitor(writer, reader.className, config), readerFlags)
        val transformed = writer.toByteArray()
        if (saveFixture) saveFixture(original, transformed, "jh.constructor.fixture.output")
        return transformed
    }

    private fun saveFixture(original: ByteArray, transformed: ByteArray, property: String) {
        System.getProperty(property)?.let { path ->
            val root = java.io.File(path).apply { mkdirs() }
            java.io.File(root, "original.class").writeBytes(original)
            java.io.File(root, "transformed.class").writeBytes(transformed)
        }
    }

    private fun checkConstructor(bytes: ByteArray) {
        val loader = object : ClassLoader(javaClass.classLoader) {
            fun define(): Class<*> = defineClass(null, bytes, 0, bytes.size)
            override fun findClass(name: String): Class<*> {
                if (name != "io.jankhunter.runtime.JankHunterHooks") throw ClassNotFoundException(name)
                val stub = ClassWriter(0).apply {
                    visit(Opcodes.V1_8, Opcodes.ACC_PUBLIC, name.replace('.', '/'), null, "java/lang/Object", null)
                    visitMethod(Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC, "enterMethod", "(JLjava/lang/String;)J", null, null).apply {
                        visitCode(); visitInsn(Opcodes.LCONST_1); visitInsn(Opcodes.LRETURN); visitMaxs(2, 3); visitEnd()
                    }
                    visitMethod(Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC, "exitMethod", "(JJ)V", null, null).apply {
                        visitCode(); visitInsn(Opcodes.RETURN); visitMaxs(0, 4); visitEnd()
                    }
                    visitEnd()
                }.toByteArray()
                return defineClass(name, stub, 0, stub.size)
            }
        }
        val constructor = loader.define().getConstructor(String::class.java)
        constructor.newInstance("1")
        constructor.newInstance("0")
        try {
            constructor.newInstance("invalid")
            error("Original argument failure was lost")
        } catch (error: InvocationTargetException) {
            assertTrue(error.cause is NumberFormatException)
            assertEquals("For input string: \"invalid\"", error.cause?.message)
        }
    }

    private fun original(beforeSuper: Boolean = true, extraAllocation: Boolean = false): ByteArray {
        val writer = ClassWriter(ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V1_8, Opcodes.ACC_PUBLIC, "fixture/PreInitialization", null, "java/lang/Object", null)
        writer.visitMethod(Opcodes.ACC_PUBLIC, "<init>", "(Ljava/lang/String;)V", null, null).apply {
            val start = Label(); val end = Label(); val handler = Label()
            visitTryCatchBlock(start, end, handler, "java/lang/Throwable")
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            if (!beforeSuper) visitMethodInsn(Opcodes.INVOKESPECIAL, "java/lang/Object", "<init>", "()V", false)
            if (extraAllocation) {
                visitTypeInsn(Opcodes.NEW, "java/lang/Object")
                visitInsn(Opcodes.DUP)
                visitMethodInsn(Opcodes.INVOKESPECIAL, "java/lang/Object", "<init>", "()V", false)
                visitInsn(Opcodes.POP)
            }
            visitLabel(start)
            visitVarInsn(Opcodes.ALOAD, 1)
            visitMethodInsn(Opcodes.INVOKESTATIC, "java/lang/Integer", "parseInt", "(Ljava/lang/String;)I", false)
            visitInsn(Opcodes.POP)
            visitLabel(end)
            if (beforeSuper) visitMethodInsn(Opcodes.INVOKESPECIAL, "java/lang/Object", "<init>", "()V", false)
            visitInsn(Opcodes.RETURN)
            visitLabel(handler)
            visitVarInsn(Opcodes.ASTORE, 2)
            visitVarInsn(Opcodes.ALOAD, 2)
            visitInsn(Opcodes.ATHROW)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun branchingOriginal(latePrologue: Boolean = false): ByteArray {
        val writer = ClassWriter(ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V1_8, Opcodes.ACC_PUBLIC, "fixture/PreInitialization", null, "java/lang/Object", null)
        writer.visitMethod(Opcodes.ACC_PUBLIC, "<init>", "(Ljava/lang/String;)V", null, null).apply {
            val alternative = Label(); val joined = Label()
            visitCode()
            if (latePrologue) {
                visitJumpInsn(Opcodes.GOTO, alternative)
                visitLabel(joined)
                visitVarInsn(Opcodes.ALOAD, 0)
                visitMethodInsn(Opcodes.INVOKESPECIAL, "java/lang/Object", "<init>", "()V", false)
                visitInsn(Opcodes.RETURN)
                visitLabel(alternative)
                visitVarInsn(Opcodes.ALOAD, 1)
                visitMethodInsn(Opcodes.INVOKESTATIC, "java/lang/Integer", "parseInt", "(Ljava/lang/String;)I", false)
                visitInsn(Opcodes.POP)
                visitJumpInsn(Opcodes.GOTO, joined)
                visitMaxs(0, 0)
                visitEnd()
                writer.visitEnd()
                return writer.toByteArray()
            }
            visitVarInsn(Opcodes.ALOAD, 1)
            visitMethodInsn(Opcodes.INVOKESTATIC, "java/lang/Integer", "parseInt", "(Ljava/lang/String;)I", false)
            visitJumpInsn(Opcodes.IFEQ, alternative)
            visitVarInsn(Opcodes.ALOAD, 0)
            visitMethodInsn(Opcodes.INVOKESPECIAL, "java/lang/Object", "<init>", "()V", false)
            visitJumpInsn(Opcodes.GOTO, joined)
            visitLabel(alternative)
            visitVarInsn(Opcodes.ALOAD, 0)
            visitMethodInsn(Opcodes.INVOKESPECIAL, "java/lang/Object", "<init>", "()V", false)
            visitLabel(joined)
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    open class KotlinArgumentParent(val value: Int)
    class KotlinPreInitialization(input: String) : KotlinArgumentParent(
        try { input.toInt() } catch (failure: NumberFormatException) { throw failure },
    )
    class KotlinConditionalArgument(input: String) : KotlinArgumentParent(if (input == "0") 0 else input.toInt())
}
