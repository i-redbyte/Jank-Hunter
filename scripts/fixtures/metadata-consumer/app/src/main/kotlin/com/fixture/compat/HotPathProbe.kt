package com.fixture.compat

import android.content.Context
import io.jankhunter.runtime.JankHunter
import java.io.File
import java.util.concurrent.atomic.AtomicInteger

object HotPathProbe {
    @JvmStatic fun verify(context: Context): String {
        val handled = AtomicInteger()
        val dropped = AtomicInteger()
        val failures = AtomicInteger()
        val maskedInside = AtomicInteger()
        val maskedOutside = AtomicInteger()
        val pipeline = HookFixture.create({ handled.incrementAndGet() },
            { dropped.incrementAndGet() }, { failures.incrementAndGet() })
        var preInit = 0
        repeat(64) {
            pipeline.logDebug({
                check(pipeline.holdsMonitor())
                preInit++
                "fixture"
            }, "fixture")
        }
        check(preInit == 64)
        pipeline.initialize {
            if (pipeline.holdsMonitor()) maskedInside.incrementAndGet()
            else maskedOutside.incrementAndGet()
        }
        check(maskedInside.get() == 64)
        var ready = 0
        repeat(1_000) {
            pipeline.logDebug({
                check(!pipeline.holdsMonitor())
                ready++
                "fixture"
            }, "fixture")
        }
        check(ready == 1_000 && maskedOutside.get() == 1_000)
        val expected = IllegalStateException("fixture")
        try {
            pipeline.logDebug({ throw expected }, "fixture")
            error("supplier exception lost")
        } catch (observed: IllegalStateException) { check(observed === expected) }
        check(handled.get() == 1_064 && failures.get() == 0 && dropped.get() == 0)
        JankHunter.flush()
        val artifacts = checkNotNull(context.getExternalFilesDir(null))
        val destination = File(artifacts, "compat-hot-path.zip")
        check(JankHunter.captureLogArchive(destination) != null && destination.length() > 0L)
        File(artifacts, "hot-path-counts.json").writeText(
            "{\"pre_init\":$preInit,\"ready\":$ready,\"masked_inside\":${maskedInside.get()}," +
                "\"masked_outside\":${maskedOutside.get()},\"handled\":${handled.get()}," +
                "\"dropped\":${dropped.get()},\"failures\":${failures.get()}}\n")
        return "hot-path-ok"
    }
}
