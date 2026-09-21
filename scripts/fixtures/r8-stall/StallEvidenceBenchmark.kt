package com.example.jhsmoke

import android.os.Looper
import android.os.Debug
import android.os.SystemClock
import android.util.Log
import io.jankhunter.annotations.JankHunterIgnore
import io.jankhunter.runtime.JankHunter

/** ART comparison harness; reflective dispatch is identical in baseline and candidate runs. */
@JankHunterIgnore
object StallEvidenceBenchmark {
    @JvmStatic
    fun run() {
        JankHunter.shutdown()
        Thread({
            val type = Class.forName("io.jankhunter.runtime.internal.system.MainThreadStallEvidence")
            val constructor = type.constructors.first { it.parameterTypes.all { parameter -> parameter == Integer.TYPE } }
            val add = type.getMethod("addSample", Array<StackTraceElement>::class.java)
            val hint = type.getMethod("getStackHint")
            val mainThread = Looper.getMainLooper().thread
            val fixed = arrayOf(
                StackTraceElement("o0.f", "getValue", "r8-map-id-9b4a62ba2e99462c59e80b83c5f560a7fe49e286807fcb81b9b1a9e72c345971", 10),
                StackTraceElement("u.n", "b", "r8-map-id-9b4a62ba2e99462c59e80b83c5f560a7fe49e286807fcb81b9b1a9e72c345971", 35),
                StackTraceElement("u.n", "c", "r8-map-id-9b4a62ba2e99462c59e80b83c5f560a7fe49e286807fcb81b9b1a9e72c345971", 56),
                StackTraceElement("a.m", "run", "r8-map-id-9b4a62ba2e99462c59e80b83c5f560a7fe49e286807fcb81b9b1a9e72c345971", 1450),
                StackTraceElement("android.os.Handler", "handleCallback", "Handler.java", 1095),
                StackTraceElement("android.os.Handler", "dispatchMessageImpl", "Handler.java", 135),
                StackTraceElement("android.os.Handler", "dispatchMessage", "Handler.java", 125),
                StackTraceElement("android.os.Looper", "loopOnce", "Looper.java", 296),
                StackTraceElement("android.os.Looper", "loop", "Looper.java", 397),
                StackTraceElement("android.app.ActivityThread", "main", "ActivityThread.java", 9523),
                StackTraceElement("java.lang.reflect.Method", "invoke", null, -2),
                StackTraceElement("com.android.internal.os.RuntimeInit\$MethodAndArgsCaller", "run", "RuntimeInit.java", 575),
                StackTraceElement("com.android.internal.os.ZygoteInit", "main", "ZygoteInit.java", 939),
            )
            for (capture in listOf(false, true)) {
                fun episode(): Int {
                    val evidence = if (constructor.parameterCount == 1) constructor.newInstance(8)
                        else constructor.newInstance(8, 1024)
                    repeat(8) { add.invoke(evidence, (if (capture) mainThread.stackTrace else fixed) as Any) }
                    return (hint.invoke(evidence) as String).length
                }
                repeat(200) { episode() }
                repeat(3) { run ->
                    val times = LongArray(1000)
                    val before = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong()
                    val cpu = Debug.threadCpuTimeNanos()
                    val started = SystemClock.elapsedRealtimeNanos()
                    var checksum = 0L
                    for (index in times.indices) {
                        val start = SystemClock.elapsedRealtimeNanos()
                        checksum += episode()
                        times[index] = SystemClock.elapsedRealtimeNanos() - start
                    }
                    val wall = SystemClock.elapsedRealtimeNanos() - started
                    val cpuNanos = Debug.threadCpuTimeNanos() - cpu
                    val allocated = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong() - before
                    times.sort()
                    val memory = Debug.MemoryInfo()
                    Debug.getMemoryInfo(memory)
                    Log.i("JHSTALLBENCH", "RESULT capture=$capture run=$run n=${times.size} " +
                        "p50ns=${times[500]} p95ns=${times[950]} p99ns=${times[990]} " +
                        "wallns=$wall cpuns=$cpuNanos allocated=$allocated pssKb=${memory.totalPss} checksum=$checksum")
                }
            }
            Log.i("JHSTALLBENCH", "EXECUTION PASS")
        }, "stall-evidence-benchmark").start()
    }
}
