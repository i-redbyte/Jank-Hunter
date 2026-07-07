package io.jankhunter.plugin.ui

import com.intellij.openapi.ui.TextFieldWithBrowseButton
import com.intellij.testFramework.fixtures.BasePlatformTestCase
import java.io.File
import java.nio.file.Files

class JankHunterToolWindowSmokeTest : BasePlatformTestCase() {
    fun testCreatesComponent() {
        val view = JankHunterToolWindow(project, enableBrowser = false)
        try {
            assertNotNull(view.component)
        } finally {
            view.dispose()
        }
    }

    fun testFillLogsFromDirectoryClearsMissingRetainedHeapDump() {
        withTempLogDir { dir ->
            File(dir, "session-app-1000-0.jhlog").writeText("{}\n")
            val missingHeap = File(dir, "retained-1000-com.example.Service-1.hprof")
            val view = JankHunterToolWindow(project, enableBrowser = false)
            try {
                view.textField("logsDirectoryField").text = dir.path
                view.textField("heapDumpField").text = missingHeap.path

                view.fillLogsFromDirectory()

                assertEquals("", view.textField("heapDumpField").text)
                assertTrue(view.textField("logsField").text.contains("session-app-1000-0.jhlog"))
            } finally {
                view.dispose()
            }
        }
    }

    fun testFillLogsFromDirectoryAutoFillsExistingHeapDump() {
        withTempLogDir { dir ->
            File(dir, "session-app-1000-0.jhlog").writeText("{}\n")
            val heap = File(dir, "retained-1000-com.example.Service-1.hprof").apply { writeText("heap") }
            val view = JankHunterToolWindow(project, enableBrowser = false)
            try {
                view.textField("logsDirectoryField").text = dir.path

                view.fillLogsFromDirectory()

                assertEquals(heap.path, view.textField("heapDumpField").text)
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

    private fun JankHunterToolWindow.fillLogsFromDirectory() {
        val method = JankHunterToolWindow::class.java.getDeclaredMethod(
            "fillLogsFromDirectory",
            Boolean::class.javaPrimitiveType!!,
        )
        method.isAccessible = true
        method.invoke(this, false)
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
