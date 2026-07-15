package io.jankhunter.gradle

import java.util.concurrent.atomic.AtomicBoolean
import org.gradle.api.DefaultTask
import org.gradle.api.logging.Logger
import org.gradle.api.provider.Property
import org.gradle.api.services.BuildService
import org.gradle.api.services.BuildServiceParameters
import org.gradle.api.tasks.Internal
import org.gradle.api.tasks.TaskAction

internal class JankHunterBuildBannerReporter(
    private val versionName: String,
) {
    private val emitted = AtomicBoolean(false)

    fun emitOnce(emit: (String) -> Unit) {
        if (emitted.compareAndSet(false, true)) {
            emit(message(versionName))
        }
    }

    fun emitOnce(logger: Logger) {
        // The enabled marker is mandatory even when Gradle is invoked with --quiet.
        emitOnce(logger::quiet)
    }

    companion object {
        private const val BORDER = "================"

        fun message(versionName: String): String {
            return "$BORDER" + "JANK HUNTER $versionName ENABLED" + BORDER
        }
    }
}

internal abstract class JankHunterBuildBannerService :
    BuildService<JankHunterBuildBannerService.Parameters> {
    interface Parameters : BuildServiceParameters {
        val versionName: Property<String>
    }

    private val reporter by lazy {
        JankHunterBuildBannerReporter(parameters.versionName.get())
    }

    fun emitOnce(logger: Logger) {
        reporter.emitOnce(logger)
    }
}

internal abstract class PrintJankHunterBuildBannerTask : DefaultTask() {
    @get:Internal
    abstract val bannerService: Property<JankHunterBuildBannerService>

    @TaskAction
    fun printBanner() {
        bannerService.get().emitOnce(logger)
    }
}
