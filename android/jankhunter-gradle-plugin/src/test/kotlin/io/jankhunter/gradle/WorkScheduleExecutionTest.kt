package io.jankhunter.gradle

import java.net.URLClassLoader
import java.nio.file.Files
import javax.tools.ToolProvider
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassWriter

class WorkScheduleExecutionTest {
    @Test fun constructorsAndDelayPreserveEvaluatedArgumentsAndOriginalFailures() {
        val root = Files.createTempDirectory("jh-work-schedules").toFile()
        try {
            val sources = mapOf(
                "kotlin.reflect.KClass" to "package kotlin.reflect; public interface KClass {}",
                "io.jankhunter.runtime.JankHunterInstrumented" to "package io.jankhunter.runtime; public @interface JankHunterInstrumented {}",
                "androidx.work.WorkRequest" to """
                    package androidx.work;
                    public class WorkRequest {
                        public static class Builder {
                            public Builder setInitialDelay(long n, java.util.concurrent.TimeUnit u) { if(n<0) throw new IllegalArgumentException("delay"); return this; }
                            public Builder setInitialDelay(java.time.Duration d) { return this; }
                        }
                    }
                """,
                "androidx.work.PeriodicWorkRequest" to """
                    package androidx.work;
                    public class PeriodicWorkRequest {
                        public static class Builder extends WorkRequest.Builder {
                            public Builder(Class c,long p,java.util.concurrent.TimeUnit u) {if(p<0) throw new IllegalArgumentException("period");}
                            public Builder(Class c,long p,java.util.concurrent.TimeUnit u,long f,java.util.concurrent.TimeUnit fu) {}
                            public Builder(Class c,java.time.Duration p) {}
                            public Builder(Class c,java.time.Duration p,java.time.Duration f) {}
                            public Builder(kotlin.reflect.KClass c,long p,java.util.concurrent.TimeUnit u) {}
                            public Builder(kotlin.reflect.KClass c,long p,java.util.concurrent.TimeUnit u,long f,java.util.concurrent.TimeUnit fu) {}
                            public Builder(kotlin.reflect.KClass c,java.time.Duration p) {}
                            public Builder(kotlin.reflect.KClass c,java.time.Duration p,java.time.Duration f) {}
                        }
                    }
                """,
                "example.Probe" to """
                    package example;
                    public class Probe {
                        public static void main(String[] args) {
                            Caller.call(30);
                            if (io.jankhunter.runtime.JankHunterHooks.period != 1800000L || io.jankhunter.runtime.JankHunterHooks.delay != 7000L) throw new AssertionError("period/delay");
                            try { Caller.call(-1); throw new AssertionError("exception swallowed"); } catch (IllegalArgumentException expected) {}
                            if (io.jankhunter.runtime.JankHunterHooks.constructors != 1) throw new AssertionError("failed constructor observed");
                            if (!"caught".equals(Caller.catchConstructor())) throw new AssertionError("constructor handler");
                            Caller.explicit();
                            if (io.jankhunter.runtime.JankHunterHooks.flex != 300000L) throw new AssertionError("flex");
                            Caller.duration();
                            if (io.jankhunter.runtime.JankHunterHooks.delay != 0L) throw new AssertionError("zero delay");
                            Caller.kclassUnit(); Caller.kclassWindow(); Caller.kclassDuration(); Caller.kclassDurationWindow();
                            if (io.jankhunter.runtime.JankHunterHooks.period != 1800000L || io.jankhunter.runtime.JankHunterHooks.flex != 300000L) throw new AssertionError("KClass");
                            System.out.println("SCHEDULE_ART_OK");
                        }
                    }
                """,
                "example.Caller" to """
                    package example;
                    public class Caller {
                        public static Object call(long period) {
                            return new androidx.work.PeriodicWorkRequest.Builder(Caller.class,period,java.util.concurrent.TimeUnit.MINUTES)
                                .setInitialDelay(7,java.util.concurrent.TimeUnit.SECONDS);
                        }
                        public static Object catchConstructor() {
                            try { return new androidx.work.PeriodicWorkRequest.Builder(Caller.class,-1,java.util.concurrent.TimeUnit.MINUTES); }
                            catch (IllegalArgumentException expected) { return "caught"; }
                        }
                        public static Object explicit() {
                            return new androidx.work.PeriodicWorkRequest.Builder(Caller.class,30,java.util.concurrent.TimeUnit.MINUTES,5,java.util.concurrent.TimeUnit.MINUTES);
                        }
                        public static Object kclassUnit() { return new androidx.work.PeriodicWorkRequest.Builder((kotlin.reflect.KClass)null,30,java.util.concurrent.TimeUnit.MINUTES); }
                        public static Object kclassWindow() { return new androidx.work.PeriodicWorkRequest.Builder((kotlin.reflect.KClass)null,30,java.util.concurrent.TimeUnit.MINUTES,5,java.util.concurrent.TimeUnit.MINUTES); }
                        public static Object kclassDuration() { return new androidx.work.PeriodicWorkRequest.Builder((kotlin.reflect.KClass)null,java.time.Duration.ofMinutes(30)); }
                        public static Object kclassDurationWindow() { return new androidx.work.PeriodicWorkRequest.Builder((kotlin.reflect.KClass)null,java.time.Duration.ofMinutes(30),java.time.Duration.ofMinutes(5)); }
                        public static Object duration() {
                            return new androidx.work.PeriodicWorkRequest.Builder(Caller.class,java.time.Duration.ofMinutes(30),java.time.Duration.ofMinutes(5))
                                .setInitialDelay(java.time.Duration.ZERO);
                        }
                    }
                """,
                "io.jankhunter.runtime.JankHunterHooks" to """
                    package io.jankhunter.runtime;
                    public class JankHunterHooks {
                        public static int constructors, delays;
                        public static long period, flex, delay;
                        public static void observeWorkBuilderPeriodCall(Object b,long p,Object u,long f,Object fu) {
                            constructors++; period=((java.util.concurrent.TimeUnit)u).toMillis(p);
                            flex=fu==null ? -1 : ((java.util.concurrent.TimeUnit)fu).toMillis(f);
                        }
                        public static void observeWorkBuilderPeriodDurationCall(Object b,Object p,Object f) {
                            constructors++; period=((java.time.Duration)p).toMillis();flex=f==null?-1:((java.time.Duration)f).toMillis();
                        }
                        public static void observeWorkBuilderDelayCall(Object b,long d,Object u) {delays++;delay=((java.util.concurrent.TimeUnit)u).toMillis(d);}
                        public static void observeWorkBuilderDelayDurationCall(Object b,Object d) {delays++;delay=((java.time.Duration)d).toMillis();}
                    }
                """,
            ).map { (name, text) -> root.resolve(name.replace('.', '/') + ".java").apply { parentFile.mkdirs();writeText(text) } }
            assertEquals(0,checkNotNull(ToolProvider.getSystemJavaCompiler()).run(null,null,null,"-d",root.path,*sources.map {it.path}.toTypedArray()))
            val original = root.resolve("example/Caller.class").readBytes()
            val oldRuntime = HookAbiIndex(mapOf("io/jankhunter/runtime/JankHunterHooks" to HookAbiClass(
                org.objectweb.asm.Opcodes.ACC_PUBLIC, "java/lang/Object", emptyList(), emptyMap(), emptyMap(),
            )), false)
            val guarded = ClassWriter(ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
            val rejected = runCatching {
                ClassReader(original).accept(JankHunterClassVisitor(oldRuntime.visitor(guarded), "example/Caller", HookConfig(
                    methodCounters=false,okhttp=false,webSockets=false,handlers=false,executors=false,coroutines=false,
                    interactionOperations=false,logSpam=false,classGraph=false,runtimeCallGraph=false,
                    classGraphDirectory="",instrumentationDiagnosticsDirectory="",workerTracing=true,
                )), ClassReader.EXPAND_FRAMES)
            }.exceptionOrNull()
            assertTrue("Missing schedule ABI must fail during transformation", rejected?.message?.contains("observeWorkBuilderPeriodCall") == true)
            for (enabled in listOf(false,true)) {
                val reader = ClassReader(original)
                val writer = ClassWriter(reader,ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
                reader.accept(JankHunterClassVisitor(writer,reader.className,HookConfig(
                    methodCounters=false,okhttp=false,webSockets=false,handlers=false,executors=false,coroutines=false,
                    interactionOperations=false,logSpam=false,classGraph=false,runtimeCallGraph=false,
                    classGraphDirectory="",instrumentationDiagnosticsDirectory="",workerTracing=enabled,
                )),ClassReader.EXPAND_FRAMES)
                root.resolve("example/Caller.class").writeBytes(writer.toByteArray())
                URLClassLoader(arrayOf(root.toURI().toURL()),javaClass.classLoader).use { loader ->
                    val caller=loader.loadClass("example.Caller")
                    val hooks=loader.loadClass("io.jankhunter.runtime.JankHunterHooks")
                    assertTrue(caller.getMethod("call",java.lang.Long.TYPE).invoke(null,30L)!=null)
                    assertEquals(if(enabled) 1 else 0,hooks.getField("constructors").getInt(null))
                    if(enabled) {
                        assertEquals(1_800_000L,hooks.getField("period").getLong(null))
                        assertEquals(-1L,hooks.getField("flex").getLong(null))
                        assertEquals(7_000L,hooks.getField("delay").getLong(null))
                    }
                    val failure=runCatching {caller.getMethod("call",java.lang.Long.TYPE).invoke(null,-1L)}.exceptionOrNull()
                    assertEquals("period",failure?.cause?.message)
                    assertEquals(if(enabled) 1 else 0,hooks.getField("constructors").getInt(null))
                    assertEquals("caught", caller.getMethod("catchConstructor").invoke(null))
                    caller.getMethod("explicit").invoke(null)
                    if(enabled) assertEquals(300_000L,hooks.getField("flex").getLong(null))
                    caller.getMethod("duration").invoke(null)
                    if(enabled) assertEquals(0L,hooks.getField("delay").getLong(null))
                    for (method in listOf("kclassUnit", "kclassWindow", "kclassDuration", "kclassDurationWindow")) {
                        caller.getMethod(method).invoke(null)
                        if (enabled) {
                            assertEquals(1_800_000L, hooks.getField("period").getLong(null))
                            assertEquals(if (method.contains("Window")) 300_000L else -1L, hooks.getField("flex").getLong(null))
                        }
                    }
                }
            }
            System.getProperty("jh.schedule.fixture.output")?.let { destination ->
                java.util.jar.JarOutputStream(java.io.File(destination).outputStream()).use { jar ->
                    root.walkTopDown().filter { it.isFile && it.extension == "class" }.forEach { file ->
                        jar.putNextEntry(java.util.jar.JarEntry(file.relativeTo(root).invariantSeparatorsPath))
                        jar.write(file.readBytes())
                        jar.closeEntry()
                    }
                }
            }
        } finally {root.deleteRecursively()}
    }
}
