package io.jankhunter.plugin.ui

import com.intellij.openapi.ui.TextFieldWithBrowseButton
import com.intellij.testFramework.fixtures.BasePlatformTestCase
import io.jankhunter.plugin.execution.JankHunterArtifactSet
import io.jankhunter.plugin.execution.JankHunterLogScope
import io.jankhunter.plugin.execution.JankHunterMode
import java.io.File
import java.nio.file.Files
import javax.swing.JComboBox

class JankHunterToolWindowSmokeTest : BasePlatformTestCase() {
    fun testDisposeIsIdempotent() {
        val view = JankHunterToolWindow(project, enableBrowser = false)

        view.dispose()
        view.dispose()
    }

    fun testCreatesComponent() {
        val view = JankHunterToolWindow(project, enableBrowser = false)
        try {
            assertNotNull(view.component)
        } finally {
            view.dispose()
        }
    }

    fun testModeSelectorExposesScorecard() {
        val view = JankHunterToolWindow(project, enableBrowser = false)
        try {
            val modes = view.comboBox("modeCombo")
            assertTrue((0 until modes.itemCount).map(modes::getItemAt).contains(JankHunterMode.SCORECARD))
        } finally {
            view.dispose()
        }
    }

    fun testArtifactSelectionAppliesDependencyInjectionCatalog() {
        val view = JankHunterToolWindow(project, enableBrowser = false)
        try {
            val catalog = "/tmp/generated/jankhunter/debug/di-catalog.jsonl"
            val method = JankHunterToolWindow::class.java.getDeclaredMethod(
                "applyArtifactSets",
                List::class.java,
                Boolean::class.javaPrimitiveType!!,
                Boolean::class.javaPrimitiveType!!,
            )
            method.isAccessible = true
            method.invoke(
                view,
                listOf(JankHunterArtifactSet(variant = "app:debug", diCatalog = catalog)),
                true,
                false,
            )

            assertEquals(catalog, view.textField("diCatalogField").text)
        } finally {
            view.dispose()
        }
    }

    fun testPrepareLogsIgnoresMissingRetainedHeapDump() {
        withTempLogDir { dir ->
            val log = File(dir, "jh-session-log.2026-07-14.0.jhlog").apply { writeText("{}\n") }
            val view = JankHunterToolWindow(project, enableBrowser = false)
            try {
                val prepared = view.prepareLogsForGenerate("", dir, JankHunterLogScope.ALL_SELECTED)

                assertNull(prepared.heapDump)
                assertEquals(log.path, prepared.input)
            } finally {
                view.dispose()
            }
        }
    }

    fun testPrepareLogsFindsExistingHeapDump() {
        withTempLogDir { dir ->
            File(dir, "jh-session-log.2026-07-14.0.jhlog").writeText("{}\n")
            val heap = File(dir, "retained-1000-com.example.Service-1.hprof").apply { writeText("heap") }
            val view = JankHunterToolWindow(project, enableBrowser = false)
            try {
                val prepared = view.prepareLogsForGenerate("", dir, JankHunterLogScope.ALL_SELECTED)

                assertEquals(heap.path, prepared.heapDump?.path)
            } finally {
                view.dispose()
            }
        }
    }

    fun testPrepareLogsRecoversMovedDownloadsFolder() {
        withTempLogDir { dir ->
            val stale = File(dir, "logs 2/jankhunter/jh-session-log.2026-07-13.7.jhlog")
            val actualDir = File(dir, "logs (2)/jankhunter").apply { mkdirs() }
            val actual = File(actualDir, "jh-session-log.2026-07-14.8.jhlog").apply { writeText("{}\n") }
            val emptyConfiguredDir = File(dir, "plugin").apply { mkdirs() }
            val view = JankHunterToolWindow(project, enableBrowser = false)
            try {
                val prepared = view.prepareLogsForGenerate(
                    stale.path,
                    emptyConfiguredDir,
                    JankHunterLogScope.ALL_SELECTED,
                )

                assertEquals(actual.path, prepared.input)
                assertEquals(actualDir.path, prepared.directory?.path)
            } finally {
                view.dispose()
            }
        }
    }

    private fun JankHunterToolWindow.textField(name: String): TextFieldWithBrowseButton {
        val field = JankHunterToolWindow::class.java.getDeclaredField(name)
        field.isAccessible = true
        return field.get(this) as TextFieldWithBrowseButton
    }

    @Suppress("UNCHECKED_CAST")
    private fun JankHunterToolWindow.comboBox(name: String): JComboBox<JankHunterMode> {
        val field = JankHunterToolWindow::class.java.getDeclaredField(name)
        field.isAccessible = true
        return field.get(this) as JComboBox<JankHunterMode>
    }

    private fun withTempLogDir(block: (File) -> Unit) {
        val dir = Files.createTempDirectory("jankhunter-plugin-ui-test").toFile()
        try {
            block(dir)
        } finally {
            dir.deleteRecursively()
        }
    }
}
