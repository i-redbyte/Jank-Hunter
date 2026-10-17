package io.jankhunter.gradle

import java.net.URLClassLoader
import java.nio.file.Files
import javax.tools.ToolProvider
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassWriter

class WorkSubmissionExecutionTest {
    @Test
    fun boundedMetadataNeverChangesOriginalResultExceptionOrInactivePath() {
        val root = Files.createTempDirectory("jh-work-submission-bytecode").toFile()
        try {
            val sources = sources().map { (name, source) ->
                root.resolve(name.replace('.', '/') + ".java").apply { parentFile.mkdirs(); writeText(source) }
            }
            val compiler = checkNotNull(ToolProvider.getSystemJavaCompiler())
            assertEquals(0, compiler.run(null, null, null, "-d", root.path, *sources.map { it.path }.toTypedArray()))
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
        val hooks = loader.loadClass("io.jankhunter.runtime.JankHunterHooks")
        val request = loader.loadClass("androidx.work.WorkRequest").getConstructor().newInstance()
        val call = loader.loadClass("example.Caller").getMethod("call", managerType, List::class.java)
        var reads = 0
        val huge = object : AbstractList<Any>() {
            override val size = Int.MAX_VALUE
            override fun get(index: Int): Any { reads++; return request }
        }
        val result = managerType.getField("result").get(manager)
        assertSame(result, call.invoke(null, manager, huge))
        assertEquals(128, reads)
        assertEquals(128, hooks.getField("observed").getInt(null))
        assertEquals(Int.MAX_VALUE - 128, hooks.getField("omitted").getInt(null))
        managerType.getField("fail").setBoolean(manager, true)
        val failure = runCatching { call.invoke(null, manager, huge) }.exceptionOrNull()
        assertEquals("original", failure?.cause?.message)
        assertEquals(128, reads)
        managerType.getField("fail").setBoolean(manager, false)
        hooks.getField("epoch").setLong(null, 0)
        assertSame(result, call.invoke(null, manager, huge))
        assertEquals(128, reads)
        hooks.getField("epoch").setLong(null, 1)
        val broken = object : AbstractList<Any>() {
            override val size = 1
            override fun get(index: Int): Any = error("metadata failed")
        }
        assertSame(result, call.invoke(null, manager, broken))
        assertEquals(1, hooks.getField("failures").getInt(null))
        managerType.getField("result").set(manager, null)
        assertNull(call.invoke(null, manager, broken))
        assertEquals(1, hooks.getField("failures").getInt(null))
    }

    private fun sources(): Map<String, String> = mapOf(
        "androidx.work.WorkRequest" to """
            package androidx.work;
            public class WorkRequest { public java.util.UUID getId() { return new java.util.UUID(-1, 7); } }
        """.trimIndent(),
        "androidx.work.PeriodicWorkRequest" to "package androidx.work; public class PeriodicWorkRequest extends WorkRequest {}",
        "androidx.work.Operation" to "package androidx.work; public interface Operation {}",
        "androidx.work.WorkManager" to """
            package androidx.work;
            public class WorkManager {
                public Operation result = new Operation() {}; public boolean fail;
                public Operation enqueue(java.util.List<WorkRequest> requests) {
                    if (fail) throw new IllegalStateException("original"); return result;
                }
            }
        """.trimIndent(),
        "example.Caller" to """
            package example;
            public class Caller {
                public static androidx.work.Operation call(androidx.work.WorkManager manager,
                        java.util.List<androidx.work.WorkRequest> requests) { return manager.enqueue(requests); }
            }
        """.trimIndent(),
        "io.jankhunter.runtime.JankHunterHooks" to """
            package io.jankhunter.runtime;
            public class JankHunterHooks {
                public static long epoch = 1; public static int observed, omitted, failures;
                public static long workSubmissionEpoch() { return epoch; }
                public static void observeWorkSubmission(long e, long most, long least, boolean periodic,
                        String name, String policy, int missing, String source) { observed++; omitted += missing; }
                public static void observeWorkSubmissionRequests(Object requests, Object name, Object policy, String source) {
                    if (epoch == 0) return;
                    java.util.List<?> list = (java.util.List<?>) requests;
                    int count = list.size();
                    for (int index = 0; index < Math.min(count, 128); index++) {
                        try {
                            ((androidx.work.WorkRequest) list.get(index)).getId();
                            observed++;
                            if (index == 0) omitted += Math.max(count - 128, 0);
                        } catch (Throwable failure) { failures++; }
                    }
                }
                public static void workSubmissionCaptureFailed(Throwable failure) { failures++; }
            }
        """.trimIndent(),
    )
}
