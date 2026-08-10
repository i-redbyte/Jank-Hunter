package io.jankhunter.plugin.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase

class JankHunterToolWindowSmokeTest : BasePlatformTestCase() {
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
