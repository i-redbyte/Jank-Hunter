package com.example.jhsmoke

import android.app.Activity
import android.os.Debug
import android.os.Handler
import android.os.Looper
import android.util.Log
import io.jankhunter.annotations.JankHunterIgnore
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterTelemetry
import java.io.File

/** Deliberate ordinary inherited-field retention; no Kotlin capture-field naming convention. */
@JankHunterIgnore
object HeapExecutionProbe {
    @JvmField var retainedHolder: Child? = null

    @JvmStatic
    fun run(activity: Activity) {
        check(JankHunter.reconfigure("heap-probe") {
            it.retainedObjectDelayMs(1000).retainedObjectForceGcEnabled(true).retainedHeapDumpEnabled(false)
                .logDirectory(File(activity.filesDir, "heap-probe-${android.os.Process.myPid()}"))
        })
        val target = Target()
        retainedHolder = Child(target)
        // This manual API supplies a source label, not a typed runtime owner. Keep it
        // explicit; unknown legacy strings must never be guessed through mapping.
        JankHunterTelemetry.watch(target, null, "com.example.jhsmoke.HeapExecutionProbe\$Child")
        Handler(Looper.getMainLooper()).postDelayed({
            val prefix = "heap-${android.os.Process.myPid()}-${System.nanoTime()}"
            val heap = File(activity.getExternalFilesDir(null), "$prefix.hprof")
            val archive = File(activity.getExternalFilesDir(null), "$prefix.zip")
            Thread({
                Debug.dumpHprofData(heap.absolutePath)
                check(JankHunter.captureLogArchiveAsync(archive) { result ->
                    checkNotNull(result) { "Heap probe archive failed" }
                    Log.i("JHHEAPR8", "EXECUTION PASS heap=$heap archive=$archive")
                })
            }, "HeapFixtureDump").start()
        }, 3000)
    }

    open class Base(@JvmField var ordinaryParent: Any?)
    class Child(target: Target) : Base(target) {
        @JvmField var ordinaryChild: Any = Any()
    }
    class Target
}
