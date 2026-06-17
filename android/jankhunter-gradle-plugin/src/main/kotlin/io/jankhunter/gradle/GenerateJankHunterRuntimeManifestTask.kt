package io.jankhunter.gradle

import org.gradle.api.DefaultTask
import org.gradle.api.file.RegularFileProperty
import org.gradle.api.provider.Property
import org.gradle.api.tasks.Input
import org.gradle.api.tasks.Optional
import org.gradle.api.tasks.OutputFile
import org.gradle.api.tasks.TaskAction

abstract class GenerateJankHunterRuntimeManifestTask : DefaultTask() {
    @get:Input
    @get:Optional
    abstract val runtimeEnabled: Property<Boolean>

    @get:Input
    abstract val retainedHeapDumpEnabled: Property<Boolean>

    @get:Input
    abstract val retainedHeapDumpMinIntervalMs: Property<Long>

    @get:Input
    abstract val retainedHeapDumpMaxCount: Property<Int>

    @get:Input
    abstract val retainedHeapDumpMinRetainedAgeMs: Property<Long>

    @get:OutputFile
    abstract val outputFile: RegularFileProperty

    @TaskAction
    fun writeManifest() {
        val file = outputFile.get().asFile
        file.parentFile.mkdirs()
        val runtimeEnabledMetadata = runtimeEnabled.orNull?.let { enabled ->
            """
                    <meta-data
                        android:name="io.jankhunter.enabled"
                        android:value="$enabled"
                        tools:replace="android:value" />
            """.trimIndent()
        }.orEmpty()
        file.writeText(
            """
            <manifest xmlns:android="http://schemas.android.com/apk/res/android"
                xmlns:tools="http://schemas.android.com/tools">
                <application>
            $runtimeEnabledMetadata
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
                    <meta-data
                        android:name="io.jankhunter.retained_heap_dump_min_retained_age_ms"
                        android:value="${retainedHeapDumpMinRetainedAgeMs.get()}"
                        tools:replace="android:value" />
                </application>
            </manifest>
            """.trimIndent() + "\n",
        )
    }
}
