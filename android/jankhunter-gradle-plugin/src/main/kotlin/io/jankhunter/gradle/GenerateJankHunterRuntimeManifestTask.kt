package io.jankhunter.gradle

import org.gradle.api.DefaultTask
import org.gradle.api.file.RegularFileProperty
import org.gradle.api.provider.Property
import org.gradle.api.tasks.Input
import org.gradle.api.tasks.OutputFile
import org.gradle.api.tasks.TaskAction

abstract class GenerateJankHunterRuntimeManifestTask : DefaultTask() {
    @get:Input
    abstract val retainedHeapDumpEnabled: Property<Boolean>

    @get:Input
    abstract val retainedHeapDumpMinIntervalMs: Property<Long>

    @get:Input
    abstract val retainedHeapDumpMaxCount: Property<Int>

    @get:OutputFile
    abstract val outputFile: RegularFileProperty

    @TaskAction
    fun writeManifest() {
        val file = outputFile.get().asFile
        file.parentFile.mkdirs()
        file.writeText(
            """
            <manifest xmlns:android="http://schemas.android.com/apk/res/android"
                xmlns:tools="http://schemas.android.com/tools">
                <application>
                    <meta-data
                        android:name="io.jankhunter.retained_heap_dump_enabled"
                        android:value="${retainedHeapDumpEnabled.get()}"
                        tools:replace="android:value" />
                    <meta-data
                        android:name="io.jankhunter.retained_heap_dump_min_interval_ms"
                        android:value="${retainedHeapDumpMinIntervalMs.get()}"
                        tools:replace="android:value" />
                    <meta-data
                        android:name="io.jankhunter.retained_heap_dump_max_count"
                        android:value="${retainedHeapDumpMaxCount.get()}"
                        tools:replace="android:value" />
                </application>
            </manifest>
            """.trimIndent() + "\n",
        )
    }
}
