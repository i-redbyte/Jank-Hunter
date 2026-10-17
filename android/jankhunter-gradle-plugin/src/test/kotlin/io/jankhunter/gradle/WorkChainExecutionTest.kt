package io.jankhunter.gradle

import java.net.URLClassLoader
import java.nio.file.Files
import javax.tools.ToolProvider
import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassWriter

class WorkChainExecutionTest {
    @Test fun publicChainCallsPreserveResultAndExceptionWhileCapturingBoundedIds() {
        val root = Files.createTempDirectory("jh-work-chain-bytecode").toFile()
        try {
            val files = sources().map { (name, source) ->
                root.resolve(name.replace('.', '/') + ".java").apply { parentFile.mkdirs(); writeText(source) }
            }
            val compiler = checkNotNull(ToolProvider.getSystemJavaCompiler())
            assertEquals(0, compiler.run(null, null, null, "-d", root.path, *files.map { it.path }.toTypedArray()))
            val caller = root.resolve("example/Caller.class")
            val reader = ClassReader(caller.readBytes())
            val writer = ClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
            val config = HookConfig(methodCounters = false, okhttp = false, webSockets = false, handlers = false,
                executors = false, coroutines = false, interactionOperations = false, logSpam = false,
                classGraph = false, runtimeCallGraph = false, classGraphDirectory = "", instrumentationDiagnosticsDirectory = "",
                workerTracing = true)
            reader.accept(JankHunterClassVisitor(writer, reader.className, config), ClassReader.EXPAND_FRAMES)
            caller.writeBytes(writer.toByteArray())
            URLClassLoader(arrayOf(root.toURI().toURL()), javaClass.classLoader).use { loader -> verify(loader) }
        } finally { root.deleteRecursively() }
    }

    private fun verify(loader: ClassLoader) {
        val managerType = loader.loadClass("androidx.work.WorkManager")
        val manager = managerType.getConstructor().newInstance()
        val requestType = loader.loadClass("androidx.work.OneTimeWorkRequest")
        val request = requestType.getConstructor(Long::class.javaPrimitiveType).newInstance(7L)
        val caller = loader.loadClass("example.Caller").getMethod("chain", managerType, requestType)
        val result = caller.invoke(null, manager, request)
        assertSame(loader.loadClass("androidx.work.WorkContinuation").getField("lastThen").get(null), result)
        val hooks = loader.loadClass("io.jankhunter.runtime.JankHunterHooks")
        assertEquals(1, hooks.getField("roots").getInt(null))
        assertEquals(1, hooks.getField("children").getInt(null))
        assertEquals("sync|KEEP", hooks.getField("rootMetadata").get(null))
        assertEquals(0, hooks.getField("failures").getInt(null))
        assertEquals(0, hooks.getField("omitted").getInt(null))
        val combineCaller = loader.loadClass("example.Caller").getMethod("combine", managerType, requestType)
        val combinedResult = combineCaller.invoke(null, manager, request)
        assertSame(loader.loadClass("androidx.work.WorkContinuation").getField("lastThen").get(null), combinedResult)
        assertEquals(1, hooks.getField("combined").getInt(null))
        assertEquals(2, hooks.getField("combinedParents").getInt(null))
        managerType.getField("fail").setBoolean(manager, true)
        val error = runCatching { caller.invoke(null, manager, request) }.exceptionOrNull()
        assertEquals("original", error?.cause?.message)
        assertEquals(3, hooks.getField("roots").getInt(null))
    }

    private fun sources() = mapOf(
        "androidx.work.WorkRequest" to """
            package androidx.work;
            public class WorkRequest {
                public final long value; public WorkRequest(long value) { this.value = value; }
                public java.util.UUID getId() { return new java.util.UUID(0, value); }
            }
        """.trimIndent(),
        "androidx.work.OneTimeWorkRequest" to """
            package androidx.work;
            public class OneTimeWorkRequest extends WorkRequest { public OneTimeWorkRequest(long value) { super(value); } }
        """.trimIndent(),
        "androidx.work.ExistingWorkPolicy" to "package androidx.work; public enum ExistingWorkPolicy { KEEP, REPLACE }",
        "androidx.work.WorkContinuation" to """
            package androidx.work;
            public class WorkContinuation {
                public static WorkContinuation lastThen;
                public WorkContinuation then(OneTimeWorkRequest request) { lastThen = new WorkContinuation(); return lastThen; }
                public static WorkContinuation combine(java.util.List<WorkContinuation> parents) { return new WorkContinuation(); }
            }
        """.trimIndent(),
        "androidx.work.WorkManager" to """
            package androidx.work;
            public class WorkManager {
                public WorkContinuation last; public boolean fail;
                public WorkContinuation beginUniqueWork(String name, ExistingWorkPolicy policy, OneTimeWorkRequest request) {
                    if (fail) throw new IllegalStateException("original");
                    last = new WorkContinuation(); return last;
                }
            }
        """.trimIndent(),
        "example.Caller" to """
            package example;
            public class Caller {
                public static androidx.work.WorkContinuation chain(androidx.work.WorkManager manager,
                        androidx.work.OneTimeWorkRequest request) {
                    return manager.beginUniqueWork("sync", androidx.work.ExistingWorkPolicy.KEEP, request).then(request);
                }
                public static androidx.work.WorkContinuation combine(androidx.work.WorkManager manager,
                        androidx.work.OneTimeWorkRequest request) {
                    androidx.work.WorkContinuation left = manager.beginUniqueWork("left", androidx.work.ExistingWorkPolicy.KEEP, request);
                    androidx.work.WorkContinuation right = manager.beginUniqueWork("right", androidx.work.ExistingWorkPolicy.KEEP, request);
                    return androidx.work.WorkContinuation.combine(java.util.Arrays.asList(left, right)).then(request);
                }
            }
        """.trimIndent(),
        "io.jankhunter.runtime.JankHunterHooks" to """
            package io.jankhunter.runtime;
            public class JankHunterHooks {
                public static int roots, children, combined, combinedParents, omitted, failures; public static String rootMetadata;
                public static long workSubmissionEpoch() { return 1; }
                public static void workChainOmitted(long epoch, int count) { omitted += count; }
                public static void observeWorkChainRootRequests(Object continuation, Object requests,
                        Object name, Object policy, String source) {
                    roots++; rootMetadata = name + "|" + ((Enum<?>) policy).name();
                }
                public static void observeWorkChainChildRequests(Object parent, Object continuation, Object requests, String source) {
                    children++;
                }
                public static void observeWorkChainCombineRequests(java.util.List<?> parents, Object continuation) {
                    combined++; combinedParents = parents.size();
                }
                public static void workSubmissionCaptureFailed(Throwable failure) { failures++; }
            }
        """.trimIndent(),
    )
}
