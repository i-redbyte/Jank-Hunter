package io.jankhunter.runtime

import android.content.Context
import android.content.ContextWrapper
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.io.File
import java.lang.management.ManagementFactory
import java.net.URLClassLoader
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import java.util.concurrent.atomic.AtomicBoolean
import java.util.function.Supplier

/** Mandatory integration entrypoint; inputs are transformed application-independent callback bytes, never a mock. */
object HotPathAdmissionCheck {
    @JvmStatic fun main(args: Array<String>) {
        check(args.size >= 2) { "Expected transformed callback fixture jar and evidence directory" }
        val jar = File(args[0])
        check(jar.isFile)
        val output = File(args[1]).apply { mkdirs() }
        val urls = (listOf(jar) + args.drop(2).map(::File)).map { it.toURI().toURL() }.toTypedArray()
        URLClassLoader(urls, javaClass.classLoader).use { loader ->
            for (enabled in listOf(false, true)) {
                for (phase in Phase.entries) checkPipeline(loader, output, enabled, phase)
            }
        }
    }

    private fun checkPipeline(loader: ClassLoader, output: File, enabled: Boolean, phase: Phase) {
        val directory = Files.createTempDirectory("jh-callback-admission").toFile()
        val consumerEntered = CountDownLatch(1)
        val releaseConsumer = CountDownLatch(1)
        val insideLazy = CountDownLatch(1)
        val ownerDone = CountDownLatch(1)
        val contenderDone = CountDownLatch(1)
        val failure = AtomicReference<Throwable?>()
        val measuredCallback = AtomicBoolean()
        val writer = AsyncLogWriterFactory().open(directory, JankHunterConfig.builder().build(), "fixture")
        val graph = RuntimeCallGraph({ 1L }, { null }, { 0L }, { 4_096 },
            consumerLoopObserver = { consumerEntered.countDown(); releaseConsumer.await() })
        JankHunter.init(TestContext(directory), JankHunterConfig.builder()
            .availableRuntimeFeatures(setOf(JankHunterRuntimeFeature.CALL_GRAPH))
            .runtimeCallGraphEnabled(enabled).autoStartCollectors(false).logDirectory(directory).build())
        val hooks = JankHunter.instrumentationHooks()
        val field = hooks.javaClass.getDeclaredField("runtimeCallGraph").apply { isAccessible = true }
        val originalGraph = field.get(hooks)
        field.set(hooks, graph)
        graph.resetFlushState(writer)
        val bridge = loader.loadClass("com.fixture.compat.HookFixture")
        val create = bridge.methods.single { it.name == "create" }
        val pipeline = create.invoke(null, Runnable {}, Runnable {}, Runnable {})
        val pipelineClass = loader.loadClass("com.fixture.callback.CallbackPipeline")
        val logDebug = pipelineClass.getMethod("logDebug", Supplier::class.java, String::class.java)
        val logInfo = pipelineClass.getMethod("logInfo", String::class.java, String::class.java)
        val holdsLock = pipelineClass.getMethod("holdsMonitor")
        val initialize = pipelineClass.getMethod("initialize", Runnable::class.java)
        val label = phase.name.lowercase()
        if (phase == Phase.DRAINING) logInfo.invoke(pipeline, "fixture", "fixture")
        if (phase == Phase.READY) initialize.invoke(pipeline, Runnable {})
        val owner = Thread({
            try {
                val lazy: () -> String = {
                    val held = holdsLock.invoke(pipeline) == true
                    check(held == (phase != Phase.READY))
                    insideLazy.countDown()
                    // Saturate only AFTER entering the real pipeline lock. Pre-filling before
                    // logDebug could instead prove a hook delay outside that lock.
                    if (enabled) repeat((RUNTIME_GRAPH_PAGE_QUEUE_CAPACITY + 1) * RUNTIME_GRAPH_PAGE_MAX_KEYS) { index ->
                        graph.recordSemantic(1L, "fixture", index.toLong() + 2L, "fixture", 1L, true)
                    }
                    val token = JankHunterHooks.enterMethod(9001L, "fixture.lazy")
                    JankHunterHooks.exitMethod(token, 9001L)
                    "fixture"
                }
                if (phase == Phase.DRAINING) initialize.invoke(pipeline, Runnable {
                    if (measuredCallback.compareAndSet(false, true)) lazy()
                })
                else logDebug.invoke(pipeline, Supplier { lazy() }, "fixture")
            } catch (error: Throwable) { failure.compareAndSet(null, error) }
            finally { ownerDone.countDown() }
        }, "fixture-owner")
        val contender = Thread({
            try {
                logInfo.invoke(pipeline, "fixture", "fixture")
            }
            catch (error: Throwable) { failure.compareAndSet(null, error) }
            finally { contenderDone.countDown() }
        }, "fixture-contender")
        try {
            check(consumerEntered.await(5L, TimeUnit.SECONDS))
            owner.start()
            check(insideLazy.await(5L, TimeUnit.SECONDS))
            contender.start()
            val returned = ownerDone.await(100L, TimeUnit.MILLISECONDS)
            // Stack capture/formatting is observer work, outside the measured producer.
            val bean = ManagementFactory.getThreadMXBean()
            val info = bean.getThreadInfo(longArrayOf(owner.id, contender.id), true, true)
            File(output, "$label-${if (enabled) "full" else "off"}-stacks.txt").writeText(info.joinToString("\n") { it?.toString() ?: "terminated" })
            check(returned) { "Instrumentation extended the application monitor until consumer admission" }
            check(contenderDone.await(100L, TimeUnit.MILLISECONDS)) { "eager callback contender did not return" }
            failure.get()?.let { throw IllegalStateException("callback fixture failed", it) }
            if (enabled) check(graph.producerCapacityLossForTest() > 0L)
            println("$label ${if (enabled) "FULL saturated" else "off"}: returned before consumer release PASS")
        } finally {
            owner.interrupt(); contender.interrupt(); releaseConsumer.countDown()
            owner.join(5_000L); contender.join(5_000L)
            graph.flushForShutdown(); graph.clear(); writer.close()
            field.set(hooks, originalGraph)
            JankHunter.shutdown(); directory.deleteRecursively()
        }
    }

    private enum class Phase { PRE_INIT, DRAINING, READY }

    private class TestContext(private val directory: File) : ContextWrapper(null) {
        override fun getApplicationContext(): Context = this
        override fun getPackageName(): String = "fixture"
        override fun getFilesDir(): File = directory
    }
}
