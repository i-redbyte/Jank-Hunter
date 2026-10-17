package io.jankhunter.gradle

import java.io.File
import java.util.concurrent.FutureTask
import java.util.concurrent.ThreadPoolExecutor
import java.util.zip.ZipFile
import java.util.zip.ZipOutputStream
import java.util.zip.ZipEntry
import org.gradle.testfixtures.ProjectBuilder
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Label
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes
import org.objectweb.asm.tree.ClassNode
import org.objectweb.asm.tree.MethodInsnNode

/** Verify the actual scoped task, including its classpath and exception table, by executing its output. */
class CausalProvenanceTransformTaskTest {
    @get:Rule val temporary = TemporaryFolder()

    @Test fun resourceBytesSurviveTheActualTransform() {
        val resource = ByteArray(1024 * 1024) { (it % 251).toByte() }
        transform(threadCaller("app/WithAsset"), resources = mapOf("assets/model.bin" to resource))
    }

    @Test fun ownAdaptersStayByteIdenticalInWholeApplication() {
        for (name in listOf(
            "io/jankhunter/runtime/integration/AndroidXJankStatsAdapter",
            "io/jankhunter/jankstats/FrameAdapter",
            "io/jankhunter/workmanager/RegistrySource",
            "io/jankhunter/okhttp3/NetworkAdapter",
        )) {
            val source = threadCaller(name)
            for (origin in listOf(1, 2)) {
                assertArrayEquals(name, source, transform(source, origin = origin, wholeApplication = true))
            }
        }
        assertEquals(1, calls(transform(threadCaller("io/jankhunter/jankstatsextra/App"),
            origin = 2, wholeApplication = true)).count { it.name == "beginThreadStart" })
    }

    @Test fun degradedMetadataWithoutAppliedHookIsStillReported() {
        val node = ClassNode().also { ClassReader(model("app/UnknownMetadata", "java/lang/Object")).accept(it, 0) }
        node.visitAnnotation(KotlinGeneratedMethodIndex.METADATA_DESCRIPTOR, true).apply {
            visit("k", 1); visit("mv", intArrayOf(99, 0)); visitEnd()
        }
        val source = ClassWriter(0).also { node.accept(it) }.toByteArray()
        var diagnostics = ""
        transform(source, support = platformClasses(Any::class.java), diagnostics = { diagnostics = it })
        assertTrue(diagnostics.contains("\"metadataStatus\":\"UNSUPPORTED_VERSION\""))
    }

    @Test fun classWithoutCausalHooksKeepsOriginalFramesWhenOptionalTypesAreMissing() {
        val source = fixture("app/OptionalNoHooks", "(Z)I", frameParent = "optional/Base") { method ->
            branch(method, { construct(it, "optional/Left") }, { construct(it, "optional/Right") })
            method.visitMethodInsn(Opcodes.INVOKEVIRTUAL, "optional/Base", "value", "()I", false)
            method.visitInsn(Opcodes.IRETURN)
        }
        assertArrayEquals(source, transform(source, support = platformClasses(Any::class.java)))
    }

    @Test fun missingFrameHierarchyPreservesInputAndReportsNoAppliedCausalHooks() {
        val original = fixture("app/OptionalWithHooks", "(ZLjava/lang/Thread;)I", frameParent = "optional/Base") { method ->
            method.visitVarInsn(Opcodes.ALOAD, 1)
            method.visitMethodInsn(Opcodes.INVOKEVIRTUAL, "java/lang/Thread", "start", "()V", false)
            branch(method, { construct(it, "optional/Left") }, { construct(it, "optional/Right") })
            method.visitMethodInsn(Opcodes.INVOKEVIRTUAL, "optional/Base", "value", "()I", false)
            method.visitInsn(Opcodes.IRETURN)
        }
        val node = ClassNode().also { ClassReader(original).accept(it, 0) }
        node.visitAnnotation(KotlinGeneratedMethodIndex.METADATA_DESCRIPTOR, true).apply {
            visit("k", 1); visit("mv", intArrayOf(99, 0)); visitEnd()
        }
        val source = ClassWriter(0).also { node.accept(it) }.toByteArray()
        var diagnostics = ""
        val result = transform(source, diagnostics = { diagnostics = it })
        assertArrayEquals(source, result)
        assertTrue(diagnostics.contains("missing_frame_type"))
        assertTrue(diagnostics.contains("kotlin_metadata_unsupported_version"))
        assertTrue(diagnostics.contains("\"hooks\":[]"))
        val loader = Loader()
        listOf(model("optional/Base", "java/lang/Object", value = 17),
            model("optional/Left", "optional/Base"), model("optional/Right", "optional/Base")).forEach(loader::define)
        val exercise = loader.define(result).getMethod("exercise", Boolean::class.javaPrimitiveType, Thread::class.java)
        val thread = object : Thread() { override fun start() = Unit }
        assertEquals(17, exercise.invoke(null, true, thread))
        assertEquals(17, exercise.invoke(null, false, thread))
    }

    @Test fun platformSuperclassMergeRemainsExecutable() {
        val source = fixture("app/Merge", "(Z)I") { method ->
            branch(method,
                { construct(it, "java/util/ArrayList") },
                { construct(it, "java/util/LinkedList") },
            )
            method.visitMethodInsn(Opcodes.INVOKEVIRTUAL, "java/util/AbstractList", "size", "()I", false)
            method.visitInsn(Opcodes.IRETURN)
        }
        val result = transform(source, support = platformClasses(ArrayList::class.java, java.util.LinkedList::class.java))
        val method = Loader().define(result).getMethod("exercise", Boolean::class.javaPrimitiveType)
        assertEquals(0, method.invoke(null, true))
        assertEquals(0, method.invoke(null, false))
    }

    @Test fun referenceArrayMergeRemainsAnArrayForAaload() {
        val source = fixture("app/MergeArray", "(Z)Ljava/lang/Object;") { method ->
            branch(method,
                { it.visitInsn(Opcodes.ICONST_1); it.visitTypeInsn(Opcodes.ANEWARRAY, "java/lang/String") },
                { it.visitInsn(Opcodes.ICONST_1); it.visitTypeInsn(Opcodes.ANEWARRAY, "java/lang/Integer") },
            )
            method.visitInsn(Opcodes.ICONST_0)
            method.visitInsn(Opcodes.AALOAD)
            method.visitInsn(Opcodes.ARETURN)
        }
        val result = transform(source, support = platformClasses(String::class.java, Int::class.javaObjectType))
        val method = Loader().define(result).getMethod("exercise", Boolean::class.javaPrimitiveType)
        assertEquals(null, method.invoke(null, true))
        assertEquals(null, method.invoke(null, false))
    }

    @Test fun compileOnlySuperclassIsUsedForFramesAndNotCopiedToTheApplication() {
        val base = model("support/Base", "java/lang/Object", value = 17)
        val left = model("support/Left", "support/Base")
        val right = model("support/Right", "support/Base")
        val source = fixture("app/CompileOnly", "(Z)I", frameParent = "support/Base") { method ->
            branch(method, { construct(it, "support/Left") }, { construct(it, "support/Right") })
            method.visitMethodInsn(Opcodes.INVOKEVIRTUAL, "support/Base", "value", "()I", false)
            method.visitInsn(Opcodes.IRETURN)
        }
        val result = transform(source, support = platformClasses(Any::class.java) + listOf(base, left, right))
        val loader = Loader()
        listOf(base, left, right).forEach(loader::define)
        val method = loader.define(result).getMethod("exercise", Boolean::class.javaPrimitiveType)
        assertEquals(17, method.invoke(null, true))
        assertEquals(17, method.invoke(null, false))
    }

    @Test fun causalTransformRecordsCausalMetadataPathway() {
        MetadataReaderMetrics.reset()
        val kotlinBytes = app.CausalKotlinProbe::class.java.getResourceAsStream("/app/CausalKotlinProbe.class")
            ?.use { it.readBytes() }
            ?: error("missing app/CausalKotlinProbe.class")
        transform(kotlinBytes, support = platformClasses(Any::class.java))
        assertTrue(
            "expected causal metadata decode",
            MetadataReaderMetrics.pathwayDecodeCount(MetadataDecodePathway.CAUSAL) >= 1,
        )
    }

    @Test fun concretePlatformOwnersRetainCancellationShutdownAndThreadHooks() {
        val source = fixture("app/PlatformOwners", "(Ljava/util/concurrent/FutureTask;Ljava/util/concurrent/ThreadPoolExecutor;Landroid/os/HandlerThread;)V") { method ->
            method.visitVarInsn(Opcodes.ALOAD, 0)
            method.visitInsn(Opcodes.ICONST_0)
            method.visitMethodInsn(Opcodes.INVOKEVIRTUAL, "java/util/concurrent/FutureTask", "cancel", "(Z)Z", false)
            method.visitInsn(Opcodes.POP)
            method.visitVarInsn(Opcodes.ALOAD, 1)
            method.visitMethodInsn(Opcodes.INVOKEVIRTUAL, "java/util/concurrent/ThreadPoolExecutor", "shutdown", "()V", false)
            method.visitVarInsn(Opcodes.ALOAD, 2)
            method.visitMethodInsn(Opcodes.INVOKEVIRTUAL, "android/os/HandlerThread", "start", "()V", false)
            method.visitInsn(Opcodes.RETURN)
        }
        val result = transform(source, support = platformClasses(FutureTask::class.java, ThreadPoolExecutor::class.java, Thread::class.java) +
            model("android/os/HandlerThread", "java/lang/Thread"))
        val hooks = calls(result).filter { it.owner == HOOKS }.map { it.name }
        assertTrue("FutureTask must resolve Future", "beginFutureCancel" in hooks)
        assertTrue("ThreadPoolExecutor must resolve ExecutorService", "beginExecutorShutdown" in hooks)
        assertTrue("HandlerThread must resolve Thread", "beginThreadStart" in hooks)
    }

    @Test fun wholeApplicationIncludesDependenciesOutsidePackagesButHonorsExplicitExclusions() {
        val source = threadCaller("vendor/Dependency")
        val included = transform(source, origin = 2, wholeApplication = true)
        assertEquals(1, calls(included).count { it.name == "beginThreadStart" })
        val excluded = transform(source, origin = 2, wholeApplication = true, excludes = setOf("vendor"))
        assertEquals(0, calls(excluded).count { it.name == "beginThreadStart" })
        val targeted = transform(source, origin = 2, wholeApplication = false)
        assertEquals(0, calls(targeted).count { it.name == "beginThreadStart" })
    }

    @Test fun causalFailureHookRunsBeforeApplicationCatchAndPreservesTheThrowable() {
        val source = fixture("app/CaughtFailure", "(Ljava/lang/Thread;)Ljava/lang/Throwable;") { method ->
            val start = Label()
            val end = Label()
            val caught = Label()
            method.visitTryCatchBlock(start, end, caught, "java/lang/IllegalStateException")
            method.visitLabel(start)
            method.visitVarInsn(Opcodes.ALOAD, 0)
            method.visitMethodInsn(Opcodes.INVOKEVIRTUAL, "java/lang/Thread", "start", "()V", false)
            method.visitLabel(end)
            method.visitInsn(Opcodes.ACONST_NULL)
            method.visitInsn(Opcodes.ARETURN)
            method.visitLabel(caught)
            method.visitInsn(Opcodes.ARETURN)
        }
        val loader = Loader()
        val hooks = loader.define(hookStub())
        val transformed = loader.define(transform(source))
        val failure = IllegalStateException("original failure")
        val thread = object : Thread() { override fun start() { throw failure } }
        assertSame(failure, transformed.getMethod("exercise", Thread::class.java).invoke(null, thread))
        assertEquals("telemetry must observe a failure even when application recovers", 1, hooks.getField("failures").getInt(null))
    }

    private fun transform(
        source: ByteArray,
        support: List<ByteArray> = platformClasses(Thread::class.java, IllegalStateException::class.java),
        origin: Int = 1,
        wholeApplication: Boolean = false,
        excludes: Set<String> = emptySet(),
        resources: Map<String, ByteArray> = emptyMap(),
        diagnostics: (String) -> Unit = {},
    ): ByteArray {
        val directory = temporary.newFolder()
        val project = ProjectBuilder.builder().withProjectDir(directory).build()
        val task = project.tasks.register("causal", CausalProvenanceTransformTask::class.java).get()
        val name = ClassReader(source).className
        val program = File(directory, "program.jar")
        writeJar(program, listOf(source), resources)
        val supportFile = File(directory, "support.jar")
        writeJar(supportFile, support)
        val catalog = File(directory, "origin.txt").apply { writeText("JHCORIGIN1\n$origin\t$name\n") }
        task.inputJars.set(listOf(project.layout.projectDirectory.file(program.name)))
        task.inputDirectories.set(emptyList())
        task.supportClasspath.from(supportFile)
        task.codeOriginCatalog.set(catalog)
        task.includePackages.set(setOf("app"))
        task.excludePackages.set(excludes)
        task.includeWholeApplication.set(wholeApplication)
        task.executors.set(true)
        task.handlers.set(true)
        task.threadLifecycle.set(true)
        task.concurrencyDiagnostics.set(true)
        task.coroutines.set(true)
        task.workerTracing.set(false)
        val output = File(directory, "transformed.jar")
        task.outputFile.set(output)
        task.transform()
        diagnostics(task.diagnosticsDirectory.get().asFile.walkTopDown().filter { it.extension == "jsonl" }.joinToString("\n") { it.readText() })
        return ZipFile(output).use { archive ->
            resources.forEach { (path, expected) ->
                assertArrayEquals(path, expected, archive.getInputStream(archive.getEntry(path)).use { it.readBytes() })
            }
            assertFalse("support classes must not be packaged", archive.entries().asSequence().any { it.name.startsWith("support/") })
            archive.getInputStream(archive.getEntry("$name.class")).readBytes()
        }
    }

    private fun threadCaller(name: String): ByteArray = fixture(name, "(Ljava/lang/Thread;)V") { method ->
        method.visitVarInsn(Opcodes.ALOAD, 0)
        method.visitMethodInsn(Opcodes.INVOKEVIRTUAL, "java/lang/Thread", "start", "()V", false)
        method.visitInsn(Opcodes.RETURN)
    }

    private fun fixture(name: String, descriptor: String, frameParent: String? = null, body: (MethodVisitor) -> Unit): ByteArray {
        val writer = object : ClassWriter(COMPUTE_FRAMES or COMPUTE_MAXS) {
            override fun getCommonSuperClass(first: String, second: String): String =
                frameParent ?: super.getCommonSuperClass(first, second)
        }
        writer.visit(Opcodes.V1_8, Opcodes.ACC_PUBLIC, name, null, "java/lang/Object", null)
        writer.visitMethod(Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC, "exercise", descriptor, null, null).apply {
            visitCode()
            body(this)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun branch(method: MethodVisitor, first: (MethodVisitor) -> Unit, second: (MethodVisitor) -> Unit) {
        val other = Label()
        val merge = Label()
        method.visitVarInsn(Opcodes.ILOAD, 0)
        method.visitJumpInsn(Opcodes.IFEQ, other)
        first(method)
        method.visitJumpInsn(Opcodes.GOTO, merge)
        method.visitLabel(other)
        second(method)
        method.visitLabel(merge)
    }

    private fun construct(method: MethodVisitor, name: String) {
        method.visitTypeInsn(Opcodes.NEW, name)
        method.visitInsn(Opcodes.DUP)
        method.visitMethodInsn(Opcodes.INVOKESPECIAL, name, "<init>", "()V", false)
    }

    private fun model(name: String, parent: String, value: Int? = null): ByteArray {
        val writer = ClassWriter(ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V1_8, Opcodes.ACC_PUBLIC, name, null, parent, null)
        writer.visitMethod(Opcodes.ACC_PUBLIC, "<init>", "()V", null, null).apply {
            visitCode(); visitVarInsn(Opcodes.ALOAD, 0)
            visitMethodInsn(Opcodes.INVOKESPECIAL, parent, "<init>", "()V", false)
            visitInsn(Opcodes.RETURN); visitMaxs(0, 0); visitEnd()
        }
        if (value != null) writer.visitMethod(Opcodes.ACC_PUBLIC, "value", "()I", null, null).apply {
            visitCode(); visitLdcInsn(value); visitInsn(Opcodes.IRETURN); visitMaxs(0, 0); visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun platformClasses(vararg roots: Class<*>): List<ByteArray> {
        val seen = linkedSetOf<Class<*>>()
        fun collect(type: Class<*>) {
            if (!seen.add(type)) return
            type.superclass?.let(::collect)
            type.interfaces.forEach(::collect)
        }
        roots.forEach(::collect)
        return seen.map { type ->
            checkNotNull(type.getResourceAsStream("/${type.name.replace('.', '/')}.class")).use { it.readBytes() }
        }
    }

    private fun writeJar(file: File, classes: List<ByteArray>, resources: Map<String, ByteArray> = emptyMap()) {
        ZipOutputStream(file.outputStream()).use { output ->
            classes.forEach { bytes ->
                output.putNextEntry(ZipEntry("${ClassReader(bytes).className}.class"))
                output.write(bytes)
                output.closeEntry()
            }
            resources.forEach { (name, bytes) ->
                output.putNextEntry(ZipEntry(name))
                output.write(bytes)
                output.closeEntry()
            }
        }
    }

    private fun calls(bytes: ByteArray): List<MethodInsnNode> = ClassNode().also {
        ClassReader(bytes).accept(it, 0)
    }.methods.flatMap { it.instructions.toArray().filterIsInstance<MethodInsnNode>() }

    private fun hookStub(): ByteArray {
        val writer = ClassWriter(ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V1_8, Opcodes.ACC_PUBLIC, HOOKS, null, "java/lang/Object", null)
        writer.visitField(Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC, "failures", "I", null, 0).visitEnd()
        mapOf(
            "beginThreadStart" to "(Ljava/lang/Thread;JLjava/lang/String;II)J",
            "threadStartReturned" to "(Ljava/lang/Thread;J)V",
            "threadStartThrew" to "(Ljava/lang/Thread;J)V",
            "observeThreadStart" to "(Ljava/lang/Thread;Ljava/lang/String;)V",
        ).forEach { (name, descriptor) ->
            writer.visitMethod(Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC, name, descriptor, null, null).apply {
                visitCode()
                if (name == "threadStartThrew") {
                    visitFieldInsn(Opcodes.GETSTATIC, HOOKS, "failures", "I")
                    visitInsn(Opcodes.ICONST_1); visitInsn(Opcodes.IADD)
                    visitFieldInsn(Opcodes.PUTSTATIC, HOOKS, "failures", "I")
                }
                if (name == "beginThreadStart") { visitInsn(Opcodes.LCONST_0); visitInsn(Opcodes.LRETURN) }
                else visitInsn(Opcodes.RETURN)
                visitMaxs(0, 0); visitEnd()
            }
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private class Loader : ClassLoader(CausalProvenanceTransformTaskTest::class.java.classLoader) {
        fun define(bytes: ByteArray): Class<*> = defineClass(null, bytes, 0, bytes.size)
    }

    private companion object { const val HOOKS = "io/jankhunter/runtime/JankHunterHooks" }
}
