package io.jankhunter.gradle

import java.lang.management.ManagementFactory
import java.util.concurrent.Callable
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import kotlin.Metadata

/** Host-only probe; the workload is real metadata decode, not a timer around a stub. */
object MetadataReaderCostProbe {
    @JvmStatic
    fun main(args: Array<String>) {
        val iterations = args.getOrNull(0)?.toInt() ?: 100_000
        val workers = args.getOrNull(1)?.toInt() ?: 1
        require(iterations > 0 && workers in 1..16)
        val metadata = checkNotNull(MetadataCostFixture::class.java.getAnnotation(Metadata::class.java))
        val cpu = ManagementFactory.getThreadMXBean()
        val allocation = cpu as? com.sun.management.ThreadMXBean
        val executor = Executors.newFixedThreadPool(workers)
        try {
            val coldStart = System.nanoTime()
            check(KotlinGeneratedMethodIndex.from(metadata).analysisStatus == KotlinMetadataAnalysisStatus.READABLE)
            val coldNs = System.nanoTime() - coldStart
            val ready = CountDownLatch(workers)
            val start = CountDownLatch(1)
            val jobs = (0 until workers).map { worker ->
                executor.submit(Callable {
                    ready.countDown()
                    check(start.await(30, TimeUnit.SECONDS))
                    fun cpuTime(): Long = if (cpu.isCurrentThreadCpuTimeSupported && cpu.isThreadCpuTimeEnabled) cpu.currentThreadCpuTime else -1L
                    fun allocatedBytes(): Long = allocation?.takeIf { it.isThreadAllocatedMemorySupported && it.isThreadAllocatedMemoryEnabled }
                        ?.getThreadAllocatedBytes(Thread.currentThread().id) ?: -1L
                    val beforeCpu = cpuTime()
                    val beforeBytes = allocatedBytes()
                    val count = iterations / workers + if (worker < iterations % workers) 1 else 0
                    repeat(count) {
                        val index = KotlinGeneratedMethodIndex.from(metadata)
                        check(index.reason("component1", "()I") == "kotlin_generated_function")
                        check(index.origin("declared", "(I)I") == KotlinMethodOrigin.DECLARED)
                    }
                    val afterCpu = cpuTime()
                    val afterBytes = allocatedBytes()
                    arrayOf(if (beforeCpu >= 0 && afterCpu >= 0) afterCpu - beforeCpu else null,
                        if (beforeBytes >= 0 && afterBytes >= 0) afterBytes - beforeBytes else null)
                })
            }
            check(ready.await(30, TimeUnit.SECONDS))
            val begin = System.nanoTime()
            start.countDown()
            val totals = jobs.map { it.get(120, TimeUnit.SECONDS) }
            val wallNs = System.nanoTime() - begin
            fun total(index: Int): Long? = totals.map { it[index] }.let { values ->
                if (values.any { it == null }) null else values.filterNotNull().sum()
            }
            println("JH_METADATA_METRICS {\"iterations\":$iterations,\"workers\":$workers," +
                "\"cold_ns\":$coldNs,\"wall_ns\":$wallNs,\"cpu_ns\":${total(0)}," +
                "\"allocated_bytes\":${total(1)}}")
        } finally {
            executor.shutdownNow()
            check(executor.awaitTermination(30, TimeUnit.SECONDS))
            IsolatedMetadataReader.release()
        }
    }
}

private data class MetadataCostFixture(val value: Int) {
    fun declared(input: Int): Int = input + value
}
