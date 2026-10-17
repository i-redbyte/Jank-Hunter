package io.jankhunter.plugin.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase

class JankHunterToolWindowSmokeTest : BasePlatformTestCase() {
    fun testNativeProcessCreationCannotRunOnEdt() {
        com.intellij.testFramework.runInEdtAndWait {
            var rejected = false
            try {
                io.jankhunter.plugin.services.JankHunterProcessRunner.create(project,
                    io.jankhunter.plugin.execution.JankHunterCommand("must-not-start", emptyList(), null))
            } catch (_: IllegalStateException) { rejected = true }
            assertTrue(rejected)
        }
    }

    fun testAbandonedUnstartedHandlerIsActuallyDestroyed() {
        var destroyed = false
        val handler = object : com.intellij.execution.process.ProcessHandler() {
            override fun destroyProcessImpl() { destroyed = true; notifyProcessTerminated(0) }
            override fun detachProcessImpl() = notifyProcessDetached()
            override fun detachIsDefault() = false
            override fun getProcessInput(): java.io.OutputStream? = null
        }
        io.jankhunter.plugin.services.JankHunterProcessRunner.discardUnstarted(handler)
        assertTrue("Destroy was queued forever waiting for startNotify", destroyed)
    }

    fun testCreatesComponent() {
        val view = JankHunterToolWindow(project, enableBrowser = false)
        try {
            assertNotNull(view.component)
            assertTrue(view.component.isVisible)
        } finally {
            view.dispose()
        }
    }

    fun testDisposeIsIdempotent() {
        val view = JankHunterToolWindow(project, enableBrowser = false)

        view.dispose()
        view.dispose()
    }

    fun testClassFilterCanOpenAdvancedMode() {
        val view = JankHunterToolWindow(project, enableBrowser = false)
        try {
            view.applyClassFilter("com.example.FeedScreen")
        } finally {
            view.dispose()
        }
    }
}
