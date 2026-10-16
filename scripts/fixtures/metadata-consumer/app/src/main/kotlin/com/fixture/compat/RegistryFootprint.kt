package com.fixture.compat

import android.content.Context
import android.os.Debug
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterManifestConfig
import io.jankhunter.runtime.JankHunterRuntimeFeature
import java.io.File

object RegistryFootprint {
    @JvmStatic fun capture(context: Context): String {
        JankHunter.shutdown()
        val builder = JankHunterManifestConfig.read(context).toBuilder()
            .concurrencyMaxTrackedEntities(4096).runtimeCallGraphEnabled(false)
            .systemSamplerEnabled(false).threadSamplerEnabled(true)
            .memorySampleIntervalMs(600_000)
        JankHunterRuntimeFeature.entries.forEach { builder.runtimeFeatureEnabled(it, false) }
        builder.runtimeFeatureEnabled(JankHunterRuntimeFeature.CONCURRENCY_DIAGNOSTICS, true)
            .runtimeFeatureEnabled(JankHunterRuntimeFeature.THREAD_SAMPLING, true)
        JankHunter.init(context, builder.build())
        try {
            val referents = Array(8192) { Thread("registry-fixture-$it") }
            val dump = File(checkNotNull(context.getExternalFilesDir(null)), "causal-registry.hprof")
            check(!dump.exists() || dump.delete())
            Debug.dumpHprofData(dump.absolutePath)
            check(referents.count { it.state == Thread.State.NEW } == 8192)
            check(dump.length() > 0L)
            return "footprint-ok"
        } finally { JankHunter.shutdown() }
    }
}
