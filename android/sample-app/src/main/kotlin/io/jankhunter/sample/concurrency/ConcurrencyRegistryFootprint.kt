package io.jankhunter.sample.concurrency

import android.content.Context
import android.os.Debug
import java.io.File

/** Uses the actual constructor instrumentation in both debug and minified release. */
internal class ConcurrencyRegistryFootprint(private val context: Context) {
    fun capture(): File {
        // Strong application references remain live across the dump. Registry retained size must
        // exclude these Thread objects reached only through its weak entries.
        val referents = Array(8192) { index -> Thread("registry-footprint-$index") }
        val dump = File(context.getExternalFilesDir(null) ?: context.filesDir, "causal-registry.hprof")
        check(!dump.exists() || dump.delete())
        Debug.dumpHprofData(dump.absolutePath)
        check(referents.count { it.state == Thread.State.NEW } == 8192)
        check(dump.length() > 0L)
        return dump
    }
}
