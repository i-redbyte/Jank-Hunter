package io.jankhunter.buildlogic

import org.gradle.api.file.DirectoryProperty
import org.gradle.api.tasks.InputDirectory
import org.gradle.api.tasks.PathSensitive
import org.gradle.api.tasks.PathSensitivity
import org.gradle.api.tasks.bundling.Tar

/** Reject incomplete offline distributions even when only the packaging task is invoked. */
abstract class JankHunterCliArchiveTask : Tar() {
    @get:InputDirectory
    @get:PathSensitive(PathSensitivity.RELATIVE)
    abstract val retraceDirectory: DirectoryProperty

    init {
        doFirst { validateRetraceBundle() }
    }

    fun validateRetraceBundle() {
        val directory = retraceDirectory.get().asFile
        for (name in listOf("r8lib-9.0.32.jar", "jankhunter-retrace.jar", "R8-LICENSE.txt", "NOTICE.txt")) {
            val file = directory.resolve(name)
            check(file.isFile && file.length() > 0L) { "Missing or empty offline Retrace bundle file: $file" }
        }
    }
}
