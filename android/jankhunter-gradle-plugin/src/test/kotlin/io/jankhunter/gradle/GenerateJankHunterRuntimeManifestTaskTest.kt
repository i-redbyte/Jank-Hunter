package io.jankhunter.gradle

import org.gradle.testfixtures.ProjectBuilder
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class GenerateJankHunterRuntimeManifestTaskTest {
    @Test
    fun writesRetainedHeapDumpMetadata() {
        val project = ProjectBuilder.builder().build()
        val task = project.tasks.register(
            "generateRuntimeManifest",
            GenerateJankHunterRuntimeManifestTask::class.java,
        ).get()
        task.outputFile.set(project.layout.buildDirectory.file("jankhunter/AndroidManifest.xml"))
        task.retainedHeapDumpEnabled.set(true)
        task.retainedHeapDumpMinIntervalMs.set(123_000L)
        task.retainedHeapDumpMaxCount.set(2)
        task.retainedHeapDumpMinRetainedAgeMs.set(45_000L)

        task.writeManifest()

        val manifest = task.outputFile.get().asFile.readText()
        assertFalse(manifest.contains("io.jankhunter.enabled"))
        assertTrue(manifest.contains("io.jankhunter.retained_heap_dump_enabled"))
        assertTrue(manifest.contains("""android:value="true""""))
        assertTrue(manifest.contains("io.jankhunter.retained_heap_dump_min_interval_ms"))
        assertTrue(manifest.contains("""android:value="123000""""))
        assertTrue(manifest.contains("io.jankhunter.retained_heap_dump_max_count"))
        assertTrue(manifest.contains("""android:value="2""""))
        assertTrue(manifest.contains("io.jankhunter.retained_heap_dump_min_retained_age_ms"))
        assertTrue(manifest.contains("""android:value="45000""""))
        assertTrue(manifest.contains("""tools:replace="android:value""""))
    }

    @Test
    fun writesRuntimeDisabledMetadataWhenAutoInitIsDisabled() {
        val project = ProjectBuilder.builder().build()
        val task = project.tasks.register(
            "generateRuntimeManifest",
            GenerateJankHunterRuntimeManifestTask::class.java,
        ).get()
        task.outputFile.set(project.layout.buildDirectory.file("jankhunter/AndroidManifest.xml"))
        task.runtimeEnabled.set(false)
        task.retainedHeapDumpEnabled.set(false)
        task.retainedHeapDumpMinIntervalMs.set(123_000L)
        task.retainedHeapDumpMaxCount.set(2)
        task.retainedHeapDumpMinRetainedAgeMs.set(45_000L)

        task.writeManifest()

        val manifest = task.outputFile.get().asFile.readText()
        assertTrue(manifest.contains("io.jankhunter.enabled"))
        assertTrue(manifest.contains("""android:value="false""""))
        assertTrue(manifest.contains("""tools:replace="android:value""""))
    }
}
